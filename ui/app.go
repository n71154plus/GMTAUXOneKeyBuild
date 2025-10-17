package ui

import (
	"fmt"

	"GMTAUXOneKeyBuild/edidhelper"
	display "GMTAUXOneKeyBuild/struct"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

type App struct {
	app         *tview.Application
	displays    []*display.Display
	mainMenu    *tview.List
	displayList *tview.List
	table       *tview.Table
	statusBar   *tview.TextView
	layout      tview.Primitive
}

func NewApp() *App {
	application := tview.NewApplication().EnableMouse(true)

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

	app := &App{
		app:         application,
		mainMenu:    mainMenu,
		displayList: displayList,
		table:       table,
		statusBar:   status,
		layout:      layout,
	}

	mainMenu.SetSelectedFunc(app.handleMainMenu)
	displayList.SetChangedFunc(app.onDisplayChanged)
	displayList.SetSelectedFunc(app.onDisplaySelected)

	application.SetInputCapture(app.handleGlobalShortcuts)
	application.SetMouseCapture(app.handleMouseCapture)

	return app
}

func (app *App) Run() error {
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

	return app.app.SetRoot(app.layout, true).SetFocus(app.mainMenu).Run()
}

func (app *App) refreshDisplays() error {
	displays, err := edidhelper.GetScreens()
	app.displays = displays
	app.populateDisplayList()

	if len(displays) == 0 {
		app.table.Clear()
		return err
	}

	lastIndex := len(displays) - 1
	app.displayList.SetCurrentItem(lastIndex)
	app.updateTable(displays[lastIndex])
	return err
}

func (app *App) populateDisplayList() {
	app.displayList.Clear()
	for i, d := range app.displays {
		shortcut := rune('0' + (i % 10))
		label := fmt.Sprintf("%s", d.AdapterName)
		app.displayList.AddItem(label, d.AdapterString, shortcut, nil)
	}
	if len(app.displays) == 0 {
		app.displayList.AddItem("<無顯示器>", "", 0, nil)
	}
}

func (app *App) updateTable(d *display.Display) {
	app.table.Clear()

	headers := []string{"欄位", "內容"}
	for col, header := range headers {
		cell := tview.NewTableCell(header).
			SetTextColor(tview.Styles.SecondaryTextColor).
			SetSelectable(false).
			SetAlign(tview.AlignCenter).
			SetExpansion(1)
		app.table.SetCell(0, col, cell)
	}

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

func (app *App) handleMainMenu(index int, mainText, _ string, _ rune) {
	switch mainText {
	case "重新偵測螢幕":
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
	case "切換至螢幕列表":
		app.app.SetFocus(app.displayList)
	case "離開":
		app.app.Stop()
	}
}

func (app *App) onDisplayChanged(index int, mainText, _ string, _ rune) {
	if index < 0 || index >= len(app.displays) {
		return
	}
	app.updateTable(app.displays[index])
	app.setStatus(fmt.Sprintf("[green]目前顯示器: %s[-]", mainText))
}

func (app *App) onDisplaySelected(index int, mainText, _ string, _ rune) {
	app.onDisplayChanged(index, mainText, "", 0)
}

func (app *App) handleGlobalShortcuts(event *tcell.EventKey) *tcell.EventKey {
	switch event.Key() {
	case tcell.KeyEsc:
		app.app.SetFocus(app.mainMenu)
		return nil
	case tcell.KeyTAB:
		if app.app.GetFocus() == app.mainMenu {
			app.app.SetFocus(app.displayList)
		} else {
			app.app.SetFocus(app.mainMenu)
		}
		return nil
	case tcell.KeyBacktab:
		if app.app.GetFocus() == app.displayList {
			app.app.SetFocus(app.mainMenu)
		} else {
			app.app.SetFocus(app.displayList)
		}
		return nil
	}
	return event
}

func (app *App) handleMouseCapture(event *tcell.EventMouse, action tview.MouseAction) (*tcell.EventMouse, tview.MouseAction) {
	if event.Buttons()&tcell.Button2 != 0 {
		app.app.SetFocus(app.mainMenu)
		return nil, action
	}
	return event, action
}

func (app *App) showModal(message string) {
	modal := tview.NewModal().
		SetText(message).
		AddButtons([]string{"OK"}).
		SetDoneFunc(func(_ int, _ string) {
			app.app.SetRoot(app.layout, true).SetFocus(app.mainMenu)
		})

	app.app.SetRoot(modal, true).SetFocus(modal)
}

func (app *App) setStatus(message string) {
	app.statusBar.SetText(message)
}
