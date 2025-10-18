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
