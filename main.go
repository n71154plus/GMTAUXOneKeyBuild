package main

import (
	"log"

	"GMTAUXOneKeyBuild/ui"
)

// main 是應用程式的進入點，負責啟動文字介面應用程式。
func main() {
	app := ui.NewApp()

	// 當使用者於主選單選擇「切換至螢幕列表」時，執行自訂行為。
	app.SetSwitchToDisplayListHandler(func(app *ui.App) {
		// 預設行為為將焦點移至螢幕列表。
		app.FocusDisplayList()
	})

	// 透過 ui.NewApp 建立整個介面的實例並執行；若發生錯誤則記錄並中止。
	if err := app.Run(); err != nil {
		log.Fatalf("failed to run application: %v", err)
	}
}
