package main

import (
	"GMTAUXOneKeyBuild/edidhelper"
	display "GMTAUXOneKeyBuild/struct"
	"fmt"
	"reflect"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

var displays []*display.Display

func main() {
	// === 初始化階段 ===
	displays = edidhelper.GetScreen()

	app := tview.NewApplication().EnableMouse(true)
	// 區域副函數
	// 營幕選單初始化
	settingsListInit := func(settingsList *tview.List) {
		settingsList.Clear()
		for i, d := range displays {
			r := rune('0' + (i % 10)) // 限制快捷鍵範圍在 '0'-'9'
			settingsList.AddItem(d.AdapterName, d.AdapterString, r, nil)
		}
	}
	// 更新表格顯示
	updateTable := func(table *tview.Table, d *display.Display) {
		table.Clear()
		v := reflect.ValueOf(*d)
		t := v.Type()
		for i := 0; i < v.NumField(); i++ {
			fieldName := t.Field(i).Name
			fieldValue := fmt.Sprintf("%v", v.Field(i).Interface())
			table.SetCell(i, 0, tview.NewTableCell(fieldName).
				SetTextColor(tview.Styles.SecondaryTextColor).
				SetSelectable(false))
			table.SetCell(i, 1, tview.NewTableCell(fieldValue).
				SetTextColor(tview.Styles.PrimaryTextColor).
				SetSelectable(false))
		}
	}
	// 主清單 (Main Menu)
	mainList := tview.NewList().
		AddItem("Start", "重新偵測連接營幕", 's', nil).
		AddItem("Settings", "設定選項", 't', nil).
		AddItem("Exit", "離開應用程式", 'e', func() { app.Stop() }).
		SetHighlightFullLine(true)
	mainList.SetBorder(true).
		SetTitle(" Main Menu ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorWhite).
		SetTitleColor(tcell.ColorYellow)

	// 設定清單 (Settings Menu)
	settingsList := tview.NewList().
		SetHighlightFullLine(true)
	settingsList.SetBorder(true).
		SetTitle(" Settings Menu ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorWhite).
		SetTitleColor(tcell.ColorYellow)
	settingsListInit(settingsList)

	// 顯示資訊表格 (Display Table)
	table := tview.NewTable().
		SetBorders(true).
		SetSelectable(true, false)

	// === UI 佈局 ===
	leftPanel := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(mainList, 0, 1, true).
		AddItem(settingsList, 0, 1, false)

	rootFlex := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(leftPanel, 0, 1, true).
		AddItem(table, 0, 1, false)

	// === 綁定事件 ===

	// 當選擇不同 Display 時更新表格
	settingsList.SetChangedFunc(func(index int, mainText, secondaryText string, shortcut rune) {
		if index >= 0 && index < len(displays) {
			updateTable(table, displays[index])
		}
	})

	// 若有顯示器資料，自動顯示最後一項
	if len(displays) > 0 {
		lastIndex := len(displays) - 1
		updateTable(table, displays[lastIndex])
		settingsList.SetCurrentItem(lastIndex)
	}

	// Main Menu 選擇事件
	mainList.SetSelectedFunc(func(ix int, mainText, secText string, shortcut rune) {
		switch mainText {
		case "Start":
			displays = edidhelper.GetScreen()
			settingsListInit(settingsList)
			showModal(app, rootFlex, "螢幕重新偵測完成！")

		case "Settings":
			app.SetRoot(settingsList, true).SetFocus(settingsList)

		case "Exit":
			app.Stop()
		}
	})

	// Settings Menu 選擇事件
	settingsList.SetSelectedFunc(func(ix int, mainText, secText string, shortcut rune) {
		switch mainText {
		case "Display":
			showModal(app, settingsList, "螢幕設定開啟")
		case "Network":
			showModal(app, settingsList, "網路設定開啟")
		case "Back":
			app.SetRoot(rootFlex, true).SetFocus(mainList)
		}
	})

	// ESC 返回主畫面
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			app.SetRoot(rootFlex, true).SetFocus(mainList)
			return nil
		}
		return event
	})

	// 滑鼠右鍵返回主畫面
	app.SetMouseCapture(func(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
		if event.Buttons()&tcell.Button2 != 0 {
			app.SetRoot(rootFlex, true).SetFocus(mainList)
			return nil, action
		}
		return event, action
	})

	// 啟動應用
	if err := app.SetRoot(rootFlex, true).SetFocus(mainList).Run(); err != nil {
		panic(err)
	}
}

// === Helper Functions ===

// 顯示彈出訊息
func showModal(app *tview.Application, returnTo tview.Primitive, message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			app.SetRoot(returnTo, true).SetFocus(returnTo)
		})
	app.SetRoot(modal, true).SetFocus(modal)
}
