-- 讀取選定顯示器的 DPCD 位址 0x0000~0x0005，並以十六進位顯示結果。
local start_addr = 0x0000
local length = 6

if not context or context.selected_display_index == 0 then
  return "尚未選擇任何顯示器，無法讀取 DPCD。"
end

set_status("[cyan]開始讀取 DPCD 資料...[-]")

local data, err = read_dpcd(start_addr, length)
if not data then
  return string.format("ReadDPCD 失敗：%s", err or "未知錯誤")
end

local bytes = {}
for i = 1, #data do
  bytes[#bytes + 1] = string.format("%02X", data[i])
end

local vendor = "未知"
if context and context.gpu and context.gpu.vendor then
  vendor = context.gpu.vendor
end

local displayName = "選定顯示器"
if context and context.selected_display then
  if context.selected_display.adapter_string then
    displayName = context.selected_display.adapter_string
  elseif context.selected_display.adapter_name then
    displayName = context.selected_display.adapter_name
  end
end

set_status("[green]DPCD 讀取完成[-]")

return string.format(
  "%s (%s) 的 DPCD[0x%04X-0x%04X]：%s",
  displayName,
  vendor,
  start_addr,
  start_addr + length - 1,
  table.concat(bytes, " ")
)
