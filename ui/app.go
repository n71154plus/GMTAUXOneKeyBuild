package ui

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"GMTAUXOneKeyBuild/edidhelper"
	"GMTAUXOneKeyBuild/gpu"
	"GMTAUXOneKeyBuild/luascripts"
	display "GMTAUXOneKeyBuild/struct"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	lua "github.com/yuin/gopher-lua"
)

// App 結構封裝了整個終端介面應用程式的狀態與元件。
type App struct {
	app                   *tview.Application // tview 的核心應用程式實例
	displays              []*display.Display // 目前抓取到的顯示器資訊列表
	mainMenu              *tview.List        // 左側主要功能選單
	displayList           *tview.List        // 顯示所有顯示器的清單
	scriptList            *tview.List        // 可執行的 Lua 腳本清單
	table                 *tview.Table       // 顯示詳細屬性的表格
	statusBar             *tview.TextView    // 底部狀態列
	layout                tview.Primitive    // 頁面佈局的根節點
	scriptsDir            string
	scripts               []luascripts.Script
	onSwitchToDisplayList func(*App)
	gpuDrivers            map[string]gpu.Driver
	gpuDetectErrs         map[string]error
	gpuDetectMu           sync.Mutex
}

// NewApp 建立一個新的 App 實例，並完成所有介面的初始化設定。
func NewApp() *App {
	// 啟用滑鼠操作的 tview 應用程式，提供更友善的互動方式。
	application := tview.NewApplication().EnableMouse(true)

	// 建立主選單，提供重新偵測、切換焦點與離開等功能。
	mainMenu := tview.NewList().
		AddItem("重新偵測螢幕", "刷新顯示器列表", 'r', nil).
		AddItem("重新載入 Lua 腳本", "重新掃描 scripts 目錄", 'l', nil).
		AddItem("切換至螢幕列表", "將焦點移到螢幕選單", 'd', nil).
		AddItem("離開", "結束應用程式", 'q', nil).
		SetHighlightFullLine(true)
	mainMenu.SetBorder(true).
		SetTitle(" Main Menu ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorWhite).
		SetTitleColor(tcell.ColorYellow)

	// 顯示器清單僅呈現主要文字，方便使用者選擇不同的顯示器。
	displayList := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true)
	displayList.SetBorder(true).
		SetTitle(" Displays ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorWhite).
		SetTitleColor(tcell.ColorYellow)

	// Lua 腳本列表，用於執行放在指定資料夾內的腳本。
	scriptList := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true)
	scriptList.SetBorder(true).
		SetTitle(" Lua Scripts ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorWhite).
		SetTitleColor(tcell.ColorYellow)

	// 建立顯示詳細資料的表格，固定第一列為標題。
	table := tview.NewTable().
		SetBorders(true).
		SetSelectable(false, false).
		SetFixed(1, 0)
	table.SetBorder(true).
		SetTitle(" Display Details ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorWhite).
		SetTitleColor(tcell.ColorYellow)

	// 狀態列顯示系統提示訊息，使用動態顏色讓訊息更明顯。
	status := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(false).
		SetWrap(false)
	status.SetBorder(true).
		SetTitle(" Status ").
		SetBorderColor(tcell.ColorWhite)

	// 左側由主選單與顯示器清單上下排列組成。
	leftPanel := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(mainMenu, 0, 1, true).
		AddItem(displayList, 0, 2, false).
		AddItem(scriptList, 0, 2, false)

	// 中央內容區包含左側功能區與右側資訊表格。
	content := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(leftPanel, 0, 1, true).
		AddItem(table, 0, 2, false)

	// 最外層佈局將內容區與狀態列上下排列。
	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(content, 0, 1, true).
		AddItem(status, 1, 0, false)

		// 將所有元件封裝在 App 結構中，方便後續操作。
	app := &App{
		app:           application,
		mainMenu:      mainMenu,
		displayList:   displayList,
		scriptList:    scriptList,
		table:         table,
		statusBar:     status,
		layout:        layout,
		scriptsDir:    "scripts",
		gpuDrivers:    make(map[string]gpu.Driver),
		gpuDetectErrs: make(map[string]error),
	}

	app.onSwitchToDisplayList = func(a *App) {
		a.FocusDisplayList()
	}

	// 綁定主選單和顯示器清單的事件處理函式。
	mainMenu.SetSelectedFunc(app.handleMainMenu)
	displayList.SetChangedFunc(app.onDisplayChanged)
	displayList.SetSelectedFunc(app.onDisplaySelected)
	scriptList.SetChangedFunc(app.onScriptChanged)
	scriptList.SetSelectedFunc(app.onScriptSelected)

	// 設定全域鍵盤與滑鼠事件，使操作更直覺。
	application.SetInputCapture(app.handleGlobalShortcuts)
	application.SetMouseCapture(app.handleMouseCapture)

	return app
}

// Run 啟動應用程式，並在執行前刷新顯示器資訊與狀態。
func (app *App) Run() error {
	// 嘗試重新整理顯示器，並依結果在狀態列顯示不同訊息。
	if err := app.refreshDisplays(); err != nil {
		if len(app.displays) == 0 {
			app.setStatus(fmt.Sprintf("[red]螢幕資訊載入失敗: %v[-]", err))
		} else {
			app.setStatus(fmt.Sprintf("[yellow]部分顯示器載入失敗: %v[-]", err))
		}
	} else if len(app.displays) == 0 {
		app.setStatus("[yellow]未偵測到任何顯示器[-]")
	} else {
		app.setStatus(fmt.Sprintf("[green]載入 %d 個顯示器[-]", len(app.displays)))
	}

	if err := app.refreshScripts(); err != nil {
		app.setStatus(fmt.Sprintf("[red]Lua 腳本載入失敗: %v[-]", err))
	}

	// 建立畫面根節點並將焦點放在主選單後開始事件迴圈。
	return app.app.SetRoot(app.layout, true).SetFocus(app.mainMenu).Run()
}

// refreshDisplays 重新取得顯示器清單並更新顯示內容。
func (app *App) refreshDisplays() error {
	// 呼叫 edidhelper 取得系統中的所有顯示器資訊。
	displays, err := edidhelper.GetScreens()
	app.displays = displays
	app.populateDisplayList()

	// 若完全沒有資料，清空表格並回傳錯誤以便顯示提醒。
	if len(displays) == 0 {
		app.table.Clear()
		return err
	}

	// 預設選擇清單中的最後一個顯示器，方便快速檢視最新項目。
	lastIndex := len(displays) - 1
	app.displayList.SetCurrentItem(lastIndex)
	app.updateTable(displays[lastIndex])
	return err
}

// populateDisplayList 將顯示器資訊填入左側清單。
func (app *App) populateDisplayList() {
	app.displayList.Clear()
	for i, d := range app.displays {
		// 以數字鍵作為快捷鍵，方便使用者快速切換。
		shortcut := rune('0' + (i % 10))
		label := fmt.Sprintf("%s", d.AdapterName)
		app.displayList.AddItem(label, d.AdapterString, shortcut, nil)
	}
	// 若沒有任何顯示器，提供佔位文字提醒使用者。
	if len(app.displays) == 0 {
		app.displayList.AddItem("<無顯示器>", "", 0, nil)
	}
}

// populateScriptList 將腳本名稱填入 Lua 腳本清單。
func (app *App) populateScriptList() {
	app.scriptList.Clear()
	if len(app.scripts) == 0 {
		app.scriptList.AddItem("<無腳本>", "請將 .lua 檔案放入 scripts 目錄", 0, nil)
		return
	}

	for _, script := range app.scripts {
		app.scriptList.AddItem(script.Name, "", 0, nil)
	}
}

// updateTable 將選定顯示器的詳細資訊填入表格中。
func (app *App) updateTable(d *display.Display) {
	app.table.Clear()

	// 先建立標題列，清楚區隔欄位與內容。
	headers := []string{"欄位", "內容"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tview.Styles.SecondaryTextColor).
			SetSelectable(false).
			SetAlign(tview.AlignCenter).
			SetExpansion(1)
		app.table.SetCell(0, col, cell)
	}

	// 將顯示器結構轉成表格列，逐一填入內容。
	rows := displayToRows(d)
	for rowIndex, row := range rows {
		nameCell := tview.NewTableCell(row[0]).
			SetTextColor(tview.Styles.SecondaryTextColor).
			SetSelectable(false)
		valueCell := tview.NewTableCell(row[1]).
			SetTextColor(tview.Styles.PrimaryTextColor).
			SetSelectable(false)
		app.table.SetCell(rowIndex+1, 0, nameCell)
		app.table.SetCell(rowIndex+1, 1, valueCell)
	}
}

// displayToRows 將顯示器資訊轉換成表格可使用的列資料。
func displayToRows(d *display.Display) [][]string {
	rows := [][]string{
		{"顯示卡名稱", d.AdapterName},
		{"顯示卡描述", d.AdapterString},
		{"裝置識別碼", d.DeviceID},
		{"製造商ID", d.ManufacturerID},
		{"產品ID", d.ProductID},
		{"序號", d.Serial},
		{"製造週次", fmt.Sprintf("%d", d.Week)},
		{"製造年份", fmt.Sprintf("%d", d.Year)},
		{"EDID 版本", d.Version},
		{"EDID 修訂版", d.Revision},
	}

	// 額外的描述欄位僅在有內容時才加入表格。
	descriptors := []struct {
		label string
		value string
	}{
		{"描述 1", d.Descriptor1},
		{"描述 2", d.Descriptor2},
		{"描述 3", d.Descriptor3},
		{"描述 4", d.Descriptor4},
	}

	for _, desc := range descriptors {
		if desc.value != "" {
			rows = append(rows, []string{desc.label, desc.value})
		}
	}

	return rows
}

// handleMainMenu 處理主選單項目的點擊或快捷鍵事件。
func (app *App) handleMainMenu(index int, mainText, _ string, _ rune) {
	switch mainText {
	case "重新偵測螢幕":
		// 重新整理顯示器並依照結果顯示對應提示訊息。
		if err := app.refreshDisplays(); err != nil {
			message := fmt.Sprintf("螢幕重新偵測時發生錯誤: %v", err)
			if len(app.displays) > 0 {
				message = fmt.Sprintf("部份顯示器載入失敗: %v", err)
			}
			app.showModal(message)
		} else if len(app.displays) == 0 {
			app.showModal("未偵測到任何顯示器")
		} else {
			app.showModal("螢幕重新偵測完成！")
			app.setStatus(fmt.Sprintf("[green]載入 %d 個顯示器[-]", len(app.displays)))
		}
	case "重新載入 Lua 腳本":
		if err := app.refreshScripts(); err != nil {
			app.showModal(fmt.Sprintf("Lua 腳本載入失敗: %v", err))
		} else if len(app.scripts) == 0 {
			app.showModal("未找到任何 Lua 腳本\n請將 .lua 檔案放入 scripts 目錄")
		} else {
			app.showModal("Lua 腳本清單已更新！")
		}
	case "切換至螢幕列表":
		// 將行為委由外部指定的處理函式執行。
		if app.onSwitchToDisplayList != nil {
			app.onSwitchToDisplayList(app)
		}
	case "離開":
		// 停止事件迴圈，結束應用程式。
		app.app.Stop()
	}
}

// onDisplayChanged 在使用者切換不同顯示器時更新表格與狀態。
func (app *App) onDisplayChanged(index int, mainText, _ string, _ rune) {
	if index < 0 || index >= len(app.displays) {
		return
	}
	// 更新表格內容並同步狀態列文字。
	app.updateTable(app.displays[index])
	app.setStatus(fmt.Sprintf("[green]目前顯示器: %s[-]", mainText))
}

// onDisplaySelected 在清單項目被確認時觸發，沿用切換邏輯。
func (app *App) onDisplaySelected(index int, mainText, _ string, _ rune) {
	app.onDisplayChanged(index, mainText, "", 0)
}

// handleGlobalShortcuts 處理全域快捷鍵，提供快速切換焦點的體驗。
func (app *App) handleGlobalShortcuts(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEsc:
		// 按下 Esc 時回到主選單。
		app.app.SetFocus(app.mainMenu)
		return nil
	case tcell.KeyTAB:
		// Tab 在主選單、顯示器清單與 Lua 腳本清單間循環切換。
		switch app.app.GetFocus() {
		case app.mainMenu:
			app.app.SetFocus(app.displayList)
		case app.displayList:
			app.app.SetFocus(app.scriptList)
		default:
			app.app.SetFocus(app.mainMenu)
		}
		return nil
	case tcell.KeyBacktab:
		// Shift+Tab 則反向切換焦點。
		switch app.app.GetFocus() {
		case app.scriptList:
			app.app.SetFocus(app.displayList)
		case app.displayList:
			app.app.SetFocus(app.mainMenu)
		default:
			app.app.SetFocus(app.scriptList)
		}
		return nil
	}
	return event
}

// handleMouseCapture 攔截滑鼠操作，支援中鍵快速回到主選單。
func (app *App) handleMouseCapture(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if event.Buttons()&tcell.Button2 != 0 {
		app.app.SetFocus(app.mainMenu)
		return nil, action
	}
	return event, action
}

// showModal 顯示提示訊息的彈出視窗，並在關閉後恢復主要佈局。
func (app *App) showModal(message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			// 關閉視窗後重新設定主畫面並將焦點放回主選單。
			app.app.SetRoot(app.layout, true).SetFocus(app.mainMenu)
		})

	app.app.SetRoot(modal, true).SetFocus(modal)
}

// setStatus 更新狀態列的文字，統一由此處集中管理。
func (app *App) setStatus(message string) {
	app.statusBar.SetText(message)
}

// FocusDisplayList 將焦點移至顯示器清單，提供外部呼叫時重複使用。
func (app *App) FocusDisplayList() {
	app.app.SetFocus(app.displayList)
}

// FocusScriptList 將焦點移至 Lua 腳本清單。
func (app *App) FocusScriptList() {
	app.app.SetFocus(app.scriptList)
}

// SetSwitchToDisplayListHandler 設定主選單「切換至螢幕列表」項目的處理函式。
func (app *App) SetSwitchToDisplayListHandler(handler func(*App)) {
	if handler == nil {
		return
	}
	app.onSwitchToDisplayList = handler
}

// refreshScripts 讀取 scripts 資料夾並更新 Lua 腳本清單。
func (app *App) refreshScripts() error {
	scripts, err := luascripts.ListScripts(app.scriptsDir)
	if err != nil {
		app.scripts = nil
		app.populateScriptList()
		return err
	}

	app.scripts = scripts
	app.populateScriptList()
	return nil
}

// onScriptChanged 在使用者切換不同腳本時更新狀態列提示。
func (app *App) onScriptChanged(index int, mainText, _ string, _ rune) {
	if index < 0 || index >= len(app.scripts) {
		return
	}
	app.setStatus(fmt.Sprintf("[yellow]選擇 Lua 腳本: %s[-]", mainText))
}

// onScriptSelected 執行清單中選取的 Lua 腳本。
func (app *App) onScriptSelected(index int, mainText, _ string, _ rune) {
	if index < 0 || index >= len(app.scripts) {
		if len(app.scripts) == 0 {
			app.showModal("請將 .lua 腳本放入 scripts 目錄後再試一次。")
		}
		return
	}

	script := app.scripts[index]
	app.setStatus(fmt.Sprintf("[yellow]執行 Lua 腳本: %s[-]", script.Name))
	go app.executeLuaScript(script)
}

// executeLuaScript 在獨立 goroutine 中執行 Lua 腳本，避免阻塞 UI。
func (app *App) executeLuaScript(script luascripts.Script) {
	driver, detectErr := app.ensureGPUDriver()

	functions := map[string]lua.LGFunction{
		"set_status": func(L *lua.LState) int {
			message := L.CheckString(1)
			app.queueSetStatus(message)
			return 0
		},
		"show_modal": func(L *lua.LState) int {
			message := L.CheckString(1)
			app.queueShowModal(message)
			return 0
		},
	}

	for name, fn := range app.luaGPUFunctions(driver, detectErr) {
		functions[name] = fn
	}

	opts := luascripts.RuntimeOptions{
		Functions: functions,
		Globals: map[string]interface{}{
			"context": app.luaContext(driver, detectErr),
		},
	}

	results, err := luascripts.ExecuteScript(script.Path, opts)
	if err != nil {
		app.queueSetStatus(fmt.Sprintf("[red]Lua 腳本失敗: %v[-]", err))
		app.queueShowModal(fmt.Sprintf("Lua 腳本「%s」執行失敗:\n%v", script.Name, err))
		return
	}

	if len(results) > 0 {
		output := formatLuaResults(results)
		if strings.TrimSpace(output) != "" {
			message := fmt.Sprintf("Lua 腳本「%s」執行結果:\n%s", script.Name, output)
			app.queueShowModal(message)
		}
	}

	app.queueSetStatus(fmt.Sprintf("[green]Lua 腳本「%s」執行完成[-]", script.Name))
}

func (app *App) luaGPUFunctions(driver gpu.Driver, detectErr error) map[string]lua.LGFunction {
	describeError := func() string {
		return app.describeGPUError(detectErr)
	}

	return map[string]lua.LGFunction{
		"read_dpcd": func(L *lua.LState) int {
			if driver == nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(describeError()))
				return 2
			}
			// 從參數取得起始位址與讀取長度。
			address := uint32(L.CheckInt(1))
			length := L.CheckInt(2)
			if length <= 0 {
				L.ArgError(2, "length must be greater than zero")
				return 0
			}
			// 使用驅動介面讀取 DPCD，並將資料轉成 Lua table。
			data, err := driver.ReadDPCD(address, uint32(length))
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(err.Error()))
				return 2
			}
			tbl := L.NewTable()
			for i, b := range data {
				tbl.RawSetInt(i+1, lua.LNumber(b))
			}
			L.Push(tbl)
			return 1
		},
		"write_dpcd": func(L *lua.LState) int {
			if driver == nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString(describeError()))
				return 2
			}
			// 從 Lua table 轉換成位元組陣列後寫入指定位址。
			address := uint32(L.CheckInt(1))
			dataTbl := L.CheckTable(2)
			data, err := tableToByteSlice(dataTbl)
			if err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString(err.Error()))
				return 2
			}
			if err := driver.WriteDPCD(address, data); err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString(err.Error()))
				return 2
			}
			L.Push(lua.LBool(true))
			return 1
		},
		"read_i2c": func(L *lua.LState) int {
			if driver == nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(describeError()))
				return 2
			}
			// I2C 讀取需要起始位址與資料長度。
			address := uint32(L.CheckInt(1))
			length := L.CheckInt(2)
			if length <= 0 {
				L.ArgError(2, "length must be greater than zero")
				return 0
			}
			data, err := driver.ReadI2C(address, uint32(length))
			if err != nil {
				L.Push(lua.LNil)
				L.Push(lua.LString(err.Error()))
				return 2
			}
			tbl := L.NewTable()
			for i, b := range data {
				tbl.RawSetInt(i+1, lua.LNumber(b))
			}
			L.Push(tbl)
			return 1
		},
		"write_i2c": func(L *lua.LState) int {
			if driver == nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString(describeError()))
				return 2
			}
			// 解析 Lua table 內容並透過驅動執行寫入。
			address := uint32(L.CheckInt(1))
			dataTbl := L.CheckTable(2)
			data, err := tableToByteSlice(dataTbl)
			if err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString(err.Error()))
				return 2
			}
			if err := driver.WriteI2C(address, data); err != nil {
				L.Push(lua.LBool(false))
				L.Push(lua.LString(err.Error()))
				return 2
			}
			L.Push(lua.LBool(true))
			return 1
		},
	}
}

func (app *App) describeGPUError(err error) string {
	// 若能取得目前顯示器的供應商，以此拼接提示訊息。
	vendor := app.selectedDisplayVendor()
	unavailable := "no compatible GPU driver available for selected display"
	if vendor != "" {
		unavailable = fmt.Sprintf("no %s GPU driver available for selected display", vendor)
	}
	if err == nil {
		return unavailable
	}
	if errors.Is(err, gpu.ErrNoDriver) {
		return unavailable
	}
	return err.Error()
}

func (app *App) ensureGPUDriver() (gpu.Driver, error) {
	// 先取得目前聚焦的顯示器，再推論應使用的 GPU 驅動。
	display := app.currentDisplay()
	vendor := app.vendorKeyForDisplay(display)
	return app.ensureGPUDriverForVendor(vendor)
}

func (app *App) ensureGPUDriverForVendor(vendor string) (gpu.Driver, error) {
	key := vendor
	if key == "" {
		// 沒有特定廠牌時使用預設索引鍵，避免 map key 為空字串。
		key = "default"
	}

	app.gpuDetectMu.Lock()
	defer app.gpuDetectMu.Unlock()

	// 若之前已嘗試過偵測，直接回傳快取的結果。
	if driver, ok := app.gpuDrivers[key]; ok || app.gpuDetectErrs[key] != nil {
		return driver, app.gpuDetectErrs[key]
	}

	var (
		driver gpu.Driver
		err    error
	)

	if vendor != "" {
		// 先嘗試以指定廠牌偵測，失敗再退回一般偵測流程。
		driver, err = gpu.DetectByName(vendor)
		if errors.Is(err, gpu.ErrNoDriver) {
			driver, err = gpu.Detect()
		}
	} else {
		driver, err = gpu.Detect()
	}

	// 將結果與錯誤都記錄起來，以利後續查詢。
	if err != nil {
		app.gpuDetectErrs[key] = err
	} else {
		app.gpuDrivers[key] = driver
		app.gpuDetectErrs[key] = nil
	}
	return driver, err
}

func formatLuaResults(values []lua.LValue) string {
	if len(values) == 0 {
		return ""
	}
	// 預先配置切片並將每個回傳值轉成字串。
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, luaValueToString(value, 0))
	}
	return strings.Join(parts, "\n")
}

func luaValueToString(value lua.LValue, depth int) string {
	switch v := value.(type) {
	case lua.LBool:
		if lua.LVIsFalse(value) {
			return "false"
		}
		return "true"
	case lua.LNumber:
		return fmt.Sprintf("%g", float64(v))
	case lua.LString:
		return string(v)
	case *lua.LNilType:
		return "nil"
	case *lua.LTable:
		if depth >= 2 {
			// 避免遞迴過深，使用省略表示。
			return "{...}"
		}
		return luaTableToString(v, depth+1)
	default:
		return value.String()
	}
}

func luaTableToString(tbl *lua.LTable, depth int) string {
	length := tbl.Len()
	if length > 0 && tbl.MaxN() == length {
		if luaTableIsByteArray(tbl) {
			// 將連續索引的表視為位元組陣列並以十六進位呈現。
			elems := make([]string, 0, length)
			for i := 1; i <= length; i++ {
				num := tbl.RawGetInt(i).(lua.LNumber)
				value := int(float64(num))
				elems = append(elems, fmt.Sprintf("0x%02X", value&0xFF))
			}
			return fmt.Sprintf("[%s]", strings.Join(elems, " "))
		}
		// 其他連續索引以陣列形式輸出。
		elems := make([]string, 0, length)
		for i := 1; i <= length; i++ {
			elems = append(elems, luaValueToString(tbl.RawGetInt(i), depth))
		}
		return fmt.Sprintf("[%s]", strings.Join(elems, ", "))
	}

	entries := []string{}
	// 非連續索引則以 key=value 格式收集。
	tbl.ForEach(func(key, value lua.LValue) {
		entry := fmt.Sprintf("%s=%s", luaValueToString(key, depth), luaValueToString(value, depth))
		entries = append(entries, entry)
	})

	if len(entries) == 0 {
		return "{}"
	}
	return fmt.Sprintf("{%s}", strings.Join(entries, ", "))
}

func luaTableIsByteArray(tbl *lua.LTable) bool {
	length := tbl.Len()
	if length == 0 || tbl.MaxN() != length {
		return false
	}
	for i := 1; i <= length; i++ {
		value := tbl.RawGetInt(i)
		num, ok := value.(lua.LNumber)
		if !ok {
			return false
		}
		f := float64(num)
		if float64(int(f)) != f || f < 0 || f > 255 {
			return false
		}
	}
	return true
}

func tableToByteSlice(tbl *lua.LTable) ([]byte, error) {
	length := tbl.Len()
	if length == 0 {
		return []byte{}, nil
	}
	data := make([]byte, length)
	for i := 1; i <= length; i++ {
		val := tbl.RawGetInt(i)
		num, ok := val.(lua.LNumber)
		if !ok {
			return nil, fmt.Errorf("table index %d is not a number", i)
		}
		floatVal := float64(num)
		if float64(int(floatVal)) != floatVal {
			return nil, fmt.Errorf("table index %d value must be an integer", i)
		}
		v := int(floatVal)
		if v < 0 || v > 255 {
			return nil, fmt.Errorf("table index %d value %d out of range", i, v)
		}
		// 通過驗證後才寫入最終的 byte 切片。
		data[i-1] = byte(v)
	}
	return data, nil
}

// luaContext 建立提供給 Lua 腳本使用的資料內容。
func (app *App) luaContext(driver gpu.Driver, detectErr error) map[string]interface{} {
	currentIndex := app.displayList.GetCurrentItem()
	// 建立一個可供 Lua 閱讀的顯示器資訊切片。
	displays := make([]interface{}, len(app.displays))
	var selectedDisplay map[string]interface{}
	for i, d := range app.displays {
		// 逐一將顯示器欄位轉換成鍵值對，方便腳本使用。
		entry := map[string]interface{}{
			"adapter_name":    d.AdapterName,
			"adapter_string":  d.AdapterString,
			"device_id":       d.DeviceID,
			"manufacturer_id": d.ManufacturerID,
			"product_id":      d.ProductID,
			"serial":          d.Serial,
			"week":            d.Week,
			"year":            d.Year,
			"version":         d.Version,
			"revision":        d.Revision,
			"descriptor1":     d.Descriptor1,
			"descriptor2":     d.Descriptor2,
			"descriptor3":     d.Descriptor3,
			"descriptor4":     d.Descriptor4,
		}
		displays[i] = entry
		if i == currentIndex {
			// 記錄目前選中的顯示器資訊，供後續填入 context。
			selectedDisplay = entry
		}
	}

	selectedIndex := currentIndex + 1
	if len(app.displays) == 0 {
		selectedIndex = 0
	}
	// context 包含顯示器清單與目前索引等摘要資訊。
	context := map[string]interface{}{
		"display_count":          len(app.displays),
		"displays":               displays,
		"selected_display_index": selectedIndex,
	}

	if selectedDisplay != nil {
		context["selected_display"] = selectedDisplay
	}

	gpuInfo := map[string]interface{}{
		"available": driver != nil,
	}
	if driver != nil {
		// 若成功取得驅動，提供其名稱給腳本識別。
		gpuInfo["driver_name"] = driver.Name()
	}
	if vendor := app.selectedDisplayVendor(); vendor != "" {
		gpuInfo["vendor"] = vendor
		context["selected_display_vendor"] = vendor
	}
	if detectErr != nil {
		// 將偵測錯誤訊息同步給腳本作為診斷資訊。
		gpuInfo["error"] = detectErr.Error()
	}
	context["gpu"] = gpuInfo

	return context
}

func (app *App) currentDisplay() *display.Display {
	index := app.displayList.GetCurrentItem()
	if index < 0 || index >= len(app.displays) {
		return nil
	}
	return app.displays[index]
}

func (app *App) vendorKeyForDisplay(d *display.Display) string {
	if d == nil {
		return ""
	}
	// 透過顯示卡描述判斷可能的廠牌，供驅動偵測使用。
	adapter := strings.ToLower(d.AdapterString)
	switch {
	case strings.Contains(adapter, "nvidia"):
		return "nvidia"
	case strings.Contains(adapter, "intel"):
		return "intel"
	default:
		return ""
	}
}

func (app *App) selectedDisplayVendor() string {
	return app.vendorKeyForDisplay(app.currentDisplay())
}

func (app *App) queueSetStatus(message string) {
	// 將更新動作排入事件迴圈，避免與 UI 執行緒競爭。
	app.app.QueueUpdateDraw(func() {
		app.setStatus(message)
	})
}

func (app *App) queueShowModal(message string) {
	// 透過 QueueUpdateDraw 確保在主執行緒中建立彈窗。
	app.app.QueueUpdateDraw(func() {
		app.showModal(message)
	})
}
