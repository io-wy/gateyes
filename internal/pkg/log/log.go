package logx

import "log"

func Errorf(format string, args ...any) {
	log.Printf("[ERROR] "+format, args...)
}
