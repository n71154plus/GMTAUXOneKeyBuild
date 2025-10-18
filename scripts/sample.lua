-- 示例腳本：顯示目前載入的顯示器數量並更新狀態列
local count = context.display_count or 0
set_status(string.format("[cyan]Lua 腳本偵測到 %d 個顯示器[-]", count))

if context.displays then
  for index, display in ipairs(context.displays) do
    print(string.format("[%d] %s - %s", index, display.adapter_name, display.device_id))
  end
end

show_modal("Lua 腳本執行完成！\n請在終端查看輸出。")
