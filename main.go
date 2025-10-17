package main

import (
	"log"

	"GMTAUXOneKeyBuild/ui"
)

// main 是應用程式的進入點，負責啟動文字介面應用程式。
func main() {
	// 透過 ui.NewApp 建立整個介面的實例並執行；若發生錯誤則記錄並中止。
	if err := ui.NewApp().Run(); err != nil {
		log.Fatalf("failed to run application: %v", err)
	}
}
