package luascripts

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	lua "github.com/yuin/gopher-lua"
)

// Script 描述一個可被執行的 Lua 腳本檔案。
type Script struct {
	Name string // 檔案名稱（不含副檔名）
	Path string // 檔案的完整路徑
}

// RuntimeOptions 用於客製化 Lua 執行環境，例如注入函式或預設變數。
type RuntimeOptions struct {
	Functions map[string]lua.LGFunction
	Globals   map[string]interface{}
}

// ListScripts 掃描指定資料夾內的 .lua 檔案，並回傳排序後的腳本清單。
func ListScripts(dir string) ([]Script, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// 目錄不存在時視為沒有腳本，回傳空結果。
			return nil, nil
		}
		return nil, err
	}

	scripts := make([]Script, 0, len(entries))
	for _, entry := range entries {
		// 僅處理檔案，忽略資料夾。
		if entry.IsDir() {
			continue
		}
		// 只挑選副檔名為 .lua 的檔案。
		if filepath.Ext(entry.Name()) != ".lua" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		name := entry.Name()
		// 移除副檔名以利顯示純檔名。
		if base := name[:len(name)-len(filepath.Ext(name))]; base != "" {
			name = base
		}
		scripts = append(scripts, Script{Name: name, Path: path})
	}

	// 以檔名排序，確保清單順序一致。
	sort.Slice(scripts, func(i, j int) bool {
		return scripts[i].Name < scripts[j].Name
	})
	return scripts, nil
}

// ExecuteScript 以新的 Lua 虛擬機執行指定腳本，並可透過選項注入函式與變數。
func ExecuteScript(path string, opts RuntimeOptions) ([]lua.LValue, error) {
	L := lua.NewState()
	defer L.Close()

	for name, fn := range opts.Functions {
		// 將 Go 函式封裝成 Lua 可呼叫的全域函式。
		L.SetGlobal(name, L.NewFunction(fn))
	}

	for name, value := range opts.Globals {
		// 把預設變數寫入 Lua 環境，讓腳本可直接使用。
		L.SetGlobal(name, toLValue(L, value))
	}

	if err := ensureExecutable(path); err != nil {
		return nil, err
	}

	if err := L.DoFile(path); err != nil {
		return nil, err
	}

	top := L.GetTop()
	if top == 0 {
		return nil, nil
	}

	// 逐一取出堆疊上的回傳值並組成結果。
	results := make([]lua.LValue, top)
	for i := 1; i <= top; i++ {
		results[i-1] = L.Get(i)
	}

	return results, nil
}

func ensureExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	mode := info.Mode()
	if mode&fs.ModeType != 0 {
		// 僅接受一般檔案，避免讀取裝置或其他特殊型態。
		return fmt.Errorf("%s is not a regular file", path)
	}
	return nil
}

func toLValue(L *lua.LState, v interface{}) lua.LValue {
	switch value := v.(type) {
	case nil:
		// nil 在 Lua 中以 LNil 表示。
		return lua.LNil
	case lua.LValue:
		// 已經是 Lua 型態時可直接回傳。
		return value
	case string:
		return lua.LString(value)
	case fmt.Stringer:
		return lua.LString(value.String())
	case bool:
		return lua.LBool(value)
	case int:
		return lua.LNumber(value)
	case int8:
		return lua.LNumber(value)
	case int16:
		return lua.LNumber(value)
	case int32:
		return lua.LNumber(value)
	case int64:
		return lua.LNumber(value)
	case uint:
		return lua.LNumber(value)
	case uint8:
		return lua.LNumber(value)
	case uint16:
		return lua.LNumber(value)
	case uint32:
		return lua.LNumber(value)
	case uint64:
		return lua.LNumber(value)
	case float32:
		return lua.LNumber(value)
	case float64:
		return lua.LNumber(value)
	case []interface{}:
		// 將一般的 slice 逐一轉換成 Lua table。
		tbl := L.NewTable()
		for i, item := range value {
			tbl.RawSetInt(i+1, toLValue(L, item))
		}
		return tbl
	case map[string]interface{}:
		// map 轉換成具有字串索引的 table。
		tbl := L.NewTable()
		for key, item := range value {
			tbl.RawSetString(key, toLValue(L, item))
		}
		return tbl
	default:
		rv := reflect.ValueOf(v)
		switch rv.Kind() {
		case reflect.Slice, reflect.Array:
			// 以反射處理其他陣列型態。
			tbl := L.NewTable()
			for i := 0; i < rv.Len(); i++ {
				tbl.RawSetInt(i+1, toLValue(L, rv.Index(i).Interface()))
			}
			return tbl
		case reflect.Map:
			// 只轉換鍵為字串的項目，與 Lua table 的限制對應。
			tbl := L.NewTable()
			for _, key := range rv.MapKeys() {
				if key.Kind() != reflect.String {
					continue
				}
				tbl.RawSetString(key.String(), toLValue(L, rv.MapIndex(key).Interface()))
			}
			return tbl
		default:
			// 其餘型態以字串形式呈現，確保不會 panic。
			return lua.LString(fmt.Sprintf("%v", v))
		}
	}
}
