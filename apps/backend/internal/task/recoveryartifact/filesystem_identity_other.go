//go:build !windows

package recoveryartifact

import (
	"fmt"
	"os"
	"reflect"
)

func filesystemObjectIdentity(_ string, info os.FileInfo) (string, bool) {
	value := reflect.ValueOf(info.Sys())
	if !value.IsValid() {
		return "", false
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", false
		}
		value = value.Elem()
	}
	device, deviceOK := reflectedUnsignedField(value, "Dev")
	inode, inodeOK := reflectedUnsignedField(value, "Ino")
	if !deviceOK || !inodeOK {
		return "", false
	}
	return fmt.Sprintf("%d:%d:%d", device, inode, uint32(info.Mode().Type())), true
}

func reflectedUnsignedField(value reflect.Value, name string) (uint64, bool) {
	field := value.FieldByName(name)
	if !field.IsValid() {
		return 0, false
	}
	switch field.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return field.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if field.Int() < 0 {
			return 0, false
		}
		return uint64(field.Int()), true
	default:
		return 0, false
	}
}
