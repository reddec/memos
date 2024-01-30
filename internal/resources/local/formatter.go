package local

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/usememos/memos/internal/util"
)

var fileKeyPattern = regexp.MustCompile(`\{[a-z]{1,9}\}`)

func (local *Local) formatFile(filename string) string {
	localStoragePath := "assets/{timestamp}_{filename}"
	if local.pattern != "" {
		localStoragePath = local.pattern
	}

	internalPath := localStoragePath
	if !strings.Contains(internalPath, "{filename}") {
		internalPath = filepath.Join(internalPath, "{filename}")
	}
	return replacePathTemplate(internalPath, filename)
}

func replacePathTemplate(path, filename string) string {
	t := time.Now()
	path = fileKeyPattern.ReplaceAllStringFunc(path, func(s string) string {
		switch s {
		case "{filename}":
			return filename
		case "{timestamp}":
			return fmt.Sprintf("%d", t.Unix())
		case "{year}":
			return fmt.Sprintf("%d", t.Year())
		case "{month}":
			return fmt.Sprintf("%02d", t.Month())
		case "{day}":
			return fmt.Sprintf("%02d", t.Day())
		case "{hour}":
			return fmt.Sprintf("%02d", t.Hour())
		case "{minute}":
			return fmt.Sprintf("%02d", t.Minute())
		case "{second}":
			return fmt.Sprintf("%02d", t.Second())
		case "{uuid}":
			return util.GenUUID()
		}
		return s
	})
	return path
}
