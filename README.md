# GMTAUX One Key Build

GMTAUX One Key Build 是一個以 Go 撰寫的終端介面 (TUI) 應用程式，
用來在 Windows 平台上快速檢視已啟用顯示器的 EDID 詳細資訊，並
提供整合的 Lua 腳本載入與執行功能，協助顯示器調校或自動化流程。

## 特色

- 🔍 **顯示器偵測**：透過 Win32 API 列舉所有啟用中的顯示器，並解析
  EDID 內容，例如製造商、產品 ID、序號與詳細定時。 
- 🧭 **滑鼠與鍵盤友善操作**：基於 [tview](https://github.com/rivo/tview)
  與 [tcell](https://github.com/gdamore/tcell)，支援滑鼠操作、快捷鍵、
  焦點切換與彈出視窗。 
- 🧩 **Lua 腳本整合**：自動掃描 `scripts/` 資料夾內的 `.lua` 檔案，允許
  使用者在介面中選擇腳本並透過 [gopher-lua](https://github.com/yuin/gopher-lua)
  執行。
- 📋 **詳細資訊表格**：以表格呈現解析後的顯示器細節，包含描述符文字與
  定時資訊，方便檢視與比對。

## 系統需求

- Go 1.25 以上版本。
- Windows 10/11（顯示器列舉與 EDID 解析僅支援 Windows；非 Windows 平台
  會回傳 `display enumeration is only supported on Windows` 錯誤）。

## 快速開始

1. **取得程式碼**
   ```bash
   git clone https://github.com/<your-org>/GMTAUXOneKeyBuild.git
   cd GMTAUXOneKeyBuild
   ```

2. **安裝依賴**
   Go modules 會在編譯時自動抓取依賴，亦可手動下載：
   ```bash
   go mod download
   ```

3. **執行應用程式**
   ```bash
   go run ./...
   ```
   或者建置後執行：
   ```bash
   go build -o gmtaux-one-key-build
   ./gmtaux-one-key-build
   ```

4. **載入 Lua 腳本**
   - 將 `.lua` 腳本放置於專案根目錄的 `scripts/` 資料夾。
   - 應用程式啟動後，於「Lua Scripts」清單中選擇腳本即可執行。
   - 可透過主選單的「重新載入 Lua 腳本」來重新掃描資料夾。

## 操作提示

- 主選單可進行重新偵測顯示器、重新載入 Lua 腳本、切換焦點以及離開程式。
- `Tab` / `Shift+Tab` 可在主選單、顯示器列表與 Lua 腳本列表之間切換焦點。
- 按下 `Esc` 或滑鼠中鍵可快速回到主選單。
- 若偵測不到任何顯示器，請確認顯示器已啟用且驅動程式正常，或檢視狀態列
  的錯誤訊息。

## 介面操作總覽

- **Main Menu**（左上）提供重新偵測螢幕、重新載入腳本與快速切換焦點等功能。
  按 `Enter` 或對應快捷鍵（`r`、`l`、`d`、`q`）即可執行。【F:ui/app.go†L40-L92】
- **Displays**（左中）列出目前偵測到的顯示器，選取後右側 `Display Details`
  表格會同步更新對應資訊。【F:ui/app.go†L49-L119】
- **Lua Scripts**（左下）顯示 `scripts/` 目錄下的所有腳本。選取後按 `Enter`
  可執行腳本；執行結果會以彈出視窗與狀態列提示呈現。【F:ui/app.go†L120-L205】
- **Status**（底部）顯示目前狀態或錯誤訊息，Lua 腳本可透過 `set_status()` 更新
  內容。【F:ui/app.go†L437-L481】
- 滑鼠點擊可直接變更焦點，滾輪可捲動清單；滑鼠中鍵可立即返回主選單。
  鍵盤操作時可使用 `Tab` 在主要區塊間循環切換焦點。【F:ui/app.go†L140-L189】

## Lua 腳本 GPU 操作 API

執行 Lua 腳本時，程式會注入數個與 GPU 輔助通道相關的函式，以便直接從腳本
存取顯示器的 DPCD 與 I²C 介面。以下函式皆會在 GPU 驅動可用時才會成功運作；
若驅動偵測失敗，函式會回傳 `nil, "錯誤訊息"` 或 `false, "錯誤訊息"`。

### DPCD 操作

- `read_dpcd(address, length)`：從指定 24-bit DPCD 起始位址讀取 `length`
  個位元組。成功時回傳一個由 1 起始、包含位元組數值的陣列表；失敗時回傳
  `nil` 與錯誤字串。【F:ui/app.go†L470-L517】
- `write_dpcd(address, dataTable)`：將 `dataTable`（以 1 起始、0~255 的整數）
  寫入指定位址。成功回傳 `true`，失敗回傳 `false` 與錯誤訊息。【F:ui/app.go†L517-L541】

範例：讀取選定顯示器 0x0000~0x0005 的 DPCD 內容並列印。【F:scripts/read_dpcd_example.lua†L1-L38】

```lua
local data, err = read_dpcd(0x0000, 6)
if not data then
  return string.format("ReadDPCD 失敗：%s", err)
end

for i = 1, #data do
  print(string.format("0x%04X = 0x%02X", 0x0000 + i - 1, data[i]))
end
```

### I²C 操作

- `read_i2c(address, length)`：從指定 I²C 裝置/暫存器讀取資料。`address`
  的低 7 位元是從屬位址（不含 R/W 位），高位元組表示暫存器位址，因此例如
  `0x50 << 0 | (0x00 << 8)` 表示從屬位址 `0x50`、暫存器 `0x00`。回傳格式與
  `read_dpcd` 相同。【F:gpu/intel_igfx_windows.go†L161-L188】【F:ui/app.go†L541-L568】
- `write_i2c(address, dataTable)`：將位元組資料寫入指定 I²C 裝置/暫存器。資料
  表需為 0~255 整數，成功時回傳 `true`，失敗時回傳 `false` 與錯誤訊息。【F:gpu/intel_igfx_windows.go†L161-L188】【F:ui/app.go†L568-L593】

編寫腳本時可搭配 `set_status("訊息")` 更新狀態列，或用 `show_modal("內容")`
 顯示執行結果提示，以提供更佳的互動體驗。【F:ui/app.go†L437-L481】

## 專案結構

| 目錄 | 說明 |
| ---- | ---- |
| `main.go` | 應用程式進入點，建立並啟動 TUI。 |
| `ui/` | 終端介面元件與互動邏輯。 |
| `edidhelper/` | Windows 顯示器列舉與 EDID 解析輔助函式。 |
| `struct/` | EDID 解析結果的資料結構與解析工具。 |
| `luascripts/` | Lua 腳本掃描與執行工具。 |
| `scripts/` | 使用者自訂 Lua 腳本放置位置（可自行新增檔案）。 |

## 授權

此專案採用原始儲存庫的授權條款。如需重新散佈或修改，請遵守授權規範。
