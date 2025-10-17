package main

import (
	"fmt"
	"log"

	"GMTAUXOneKeyBuild/edidhelper"
	display "GMTAUXOneKeyBuild/struct"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type uiApp struct {
	app         *tview.Application
	displays    []*display.Display
	mainMenu    *tview.List
	displayList *tview.List
	table       *tview.Table
	statusBar   *tview.TextView
	layout      tview.Primitive
}

func main() {
	if err := newUIApp().run(); err != nil {
		log.Fatalf("failed to run application: %v", err)
	}
}

func newUIApp() *uiApp {
	app := tview.NewApplication().EnableMouse(true)

	mainMenu := tview.NewList().
		AddItem("重新偵測螢幕", "刷新顯示器列表", 'r', nil).
		AddItem("切換至螢幕列表", "將焦點移到螢幕選單", 'd', nil).
		AddItem("離開", "結束應用程式", 'q', nil).
		SetHighlightFullLine(true)
	mainMenu.SetBorder(true).
		SetTitle(" Main Menu ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorWhite).
		SetTitleColor(tcell.ColorYellow)

	displayList := tview.NewList().
		ShowSecondaryText(false).
		SetHighlightFullLine(true)
	displayList.SetBorder(true).
		SetTitle(" Displays ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorWhite).
		SetTitleColor(tcell.ColorYellow)

	table := tview.NewTable().
		SetBorders(true).
		SetSelectable(false, false).
		SetFixed(1, 0)
	table.SetBorder(true).
		SetTitle(" Display Details ").
		SetTitleAlign(tview.AlignCenter).
		SetBorderColor(tcell.ColorWhite).
		SetTitleColor(tcell.ColorYellow)

	status := tview.NewTextView().
		SetDynamicColors(true).
		SetRegions(false).
		SetWrap(false)
	status.SetBorder(true).
		SetTitle(" Status ").
		SetBorderColor(tcell.ColorWhite)

	leftPanel := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(mainMenu, 0, 1, true).
		AddItem(displayList, 0, 2, false)

	content := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(leftPanel, 0, 1, true).
		AddItem(table, 0, 2, false)

	layout := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(content, 0, 1, true).
		AddItem(status, 1, 0, false)

	ui := &uiApp{
		app:         app,
		mainMenu:    mainMenu,
		displayList: displayList,
		table:       table,
		statusBar:   status,
		layout:      layout,
	}

	mainMenu.SetSelectedFunc(ui.handleMainMenu)
	displayList.SetChangedFunc(ui.onDisplayChanged)
	displayList.SetSelectedFunc(ui.onDisplaySelected)

	app.SetInputCapture(ui.handleGlobalShortcuts)
	app.SetMouseCapture(ui.handleMouseCapture)

	return ui
}

func (ui *uiApp) run() error {
	if err := ui.refreshDisplays(); err != nil {
		if len(ui.displays) == 0 {
			ui.setStatus(fmt.Sprintf("[red]螢幕資訊載入失敗: %v[-]", err))
		} else {
			ui.setStatus(fmt.Sprintf("[yellow]部分顯示器載入失敗: %v[-]", err))
		}
	} else if len(ui.displays) == 0 {
		ui.setStatus("[yellow]未偵測到任何顯示器[-]")
	} else {
		ui.setStatus(fmt.Sprintf("[green]載入 %d 個顯示器[-]", len(ui.displays)))
	}

	return ui.app.SetRoot(ui.layout, true).SetFocus(ui.mainMenu).Run()
}

func (ui *uiApp) refreshDisplays() error {
	displays, err := edidhelper.GetScreens()
	ui.displays = displays
	ui.populateDisplayList()

	if len(displays) == 0 {
		ui.table.Clear()
		return err
	}

	lastIndex := len(displays) - 1
	ui.displayList.SetCurrentItem(lastIndex)
	ui.updateTable(displays[lastIndex])
	return err
}

func (ui *uiApp) populateDisplayList() {
	ui.displayList.Clear()
	for i, d := range ui.displays {
		shortcut := rune('0' + (i % 10))
		label := fmt.Sprintf("%s", d.AdapterName)
		ui.displayList.AddItem(label, d.AdapterString, shortcut, nil)
	}
	if len(ui.displays) == 0 {
		ui.displayList.AddItem("<無顯示器>", "", 0, nil)
	}
}

func (ui *uiApp) updateTable(d *display.Display) {
	ui.table.Clear()

	headers := []string{"欄位", "內容"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tview.Styles.SecondaryTextColor).
			SetSelectable(false).
			SetAlign(tview.AlignCenter).
			SetExpansion(1)
		ui.table.SetCell(0, col, cell)
	}

	rows := displayToRows(d)
	for rowIndex, row := range rows {
		nameCell := tview.NewTableCell(row[0]).
			SetTextColor(tview.Styles.SecondaryTextColor).
			SetSelectable(false)
		valueCell := tview.NewTableCell(row[1]).
			SetTextColor(tview.Styles.PrimaryTextColor).
			SetSelectable(false)
		ui.table.SetCell(rowIndex+1, 0, nameCell)
		ui.table.SetCell(rowIndex+1, 1, valueCell)
	}
}

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

func (ui *uiApp) handleMainMenu(index int, mainText, _ string, _ rune) {
	switch mainText {
	case "重新偵測螢幕":
		if err := ui.refreshDisplays(); err != nil {
			message := fmt.Sprintf("螢幕重新偵測時發生錯誤: %v", err)
			if len(ui.displays) > 0 {
				message = fmt.Sprintf("部份顯示器載入失敗: %v", err)
			}
			ui.showModal(message)
		} else if len(ui.displays) == 0 {
			ui.showModal("未偵測到任何顯示器")
		} else {
			ui.showModal("螢幕重新偵測完成！")
			ui.setStatus(fmt.Sprintf("[green]載入 %d 個顯示器[-]", len(ui.displays)))
		}
	case "切換至螢幕列表":
		ui.app.SetFocus(ui.displayList)
	case "離開":
		ui.app.Stop()
	}
}

func (ui *uiApp) onDisplayChanged(index int, mainText, _ string, _ rune) {
	if index < 0 || index >= len(ui.displays) {
		return
	}
	ui.updateTable(ui.displays[index])
	ui.setStatus(fmt.Sprintf("[green]目前顯示器: %s[-]", mainText))
}

func (ui *uiApp) onDisplaySelected(index int, mainText, _ string, _ rune) {
	ui.onDisplayChanged(index, mainText, "", 0)
}

func (ui *uiApp) handleGlobalShortcuts(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEsc:
		ui.app.SetFocus(ui.mainMenu)
		return nil
	case tcell.KeyTAB:
		if ui.app.GetFocus() == ui.mainMenu {
			ui.app.SetFocus(ui.displayList)
		} else {
			ui.app.SetFocus(ui.mainMenu)
		}
		return nil
	case tcell.KeyBacktab:
		if ui.app.GetFocus() == ui.displayList {
			ui.app.SetFocus(ui.mainMenu)
		} else {
			ui.app.SetFocus(ui.displayList)
		}
		return nil
	}
	return event
}

func (ui *uiApp) handleMouseCapture(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if event.Buttons()&tcell.Button2 != 0 {
		ui.app.SetFocus(ui.mainMenu)
		return nil, action
	}
	return event, action
}

func (ui *uiApp) showModal(message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			ui.app.SetRoot(ui.layout, true).SetFocus(ui.mainMenu)
		})

	ui.app.SetRoot(modal, true).SetFocus(modal)
}

func (ui *uiApp) setStatus(message string) {
	ui.statusBar.SetText(message)
}
