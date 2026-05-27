package utils

import (
	"sync"
	"time"
)

var (
	shanghaiLoc     *time.Location
	shanghaiLocOnce sync.Once
)

// ShanghaiLocation 返回 Asia/Shanghai 时区（懒加载，仅初始化一次）
func ShanghaiLocation() *time.Location {
	shanghaiLocOnce.Do(func() {
		loc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			loc = time.FixedZone("CST", 8*3600)
			LogWarn("TIME", "Failed to load Asia/Shanghai, using FixedZone CST+8")
		}
		shanghaiLoc = loc
		_ = err
	})
	return shanghaiLoc
}
