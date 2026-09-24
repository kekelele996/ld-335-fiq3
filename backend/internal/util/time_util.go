package util

import "time"

// cstZone Asia/Shanghai 固定时区（+08:00），与 DB 会话 TimeZone=Asia/Shanghai 一致。
var cstZone = time.FixedZone("CST", 8*3600)

func timeNowDate() string {
	return time.Now().Format("20060102")
}

// NowTime 返回当前时间。
func NowTime() time.Time { return time.Now() }

// TodayDate 返回 Asia/Shanghai 时区的今天日期（与 DB 会话 TimeZone=Asia/Shanghai 一致）。
func TodayDate() string {
	return time.Now().In(cstZone).Format("2006-01-02")
}

// DateLayout 对账日期格式（自然日，YYYY-MM-DD）。
const DateLayout = "2006-01-02"

// NormalizeDate 校验并归一化对账日期参数；空串视为今天。
// 仅接受 YYYY-MM-DD 格式，非法日期返回错误。
func NormalizeDate(date string) (string, error) {
	if date == "" {
		return TodayDate(), nil
	}
	if t, err := time.ParseInLocation(DateLayout, date, cstZone); err != nil {
		return "", ErrInvalidDate
	} else if t.Format(DateLayout) != date {
		return "", ErrInvalidDate
	}
	return date, nil
}

// DayRange 返回 Asia/Shanghai 时区某自然日（YYYY-MM-DD）的半开区间 [00:00, 次日00:00)。
func DayRange(date string) (time.Time, time.Time, error) {
	start, err := time.ParseInLocation(DateLayout, date, cstZone)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return start, start.Add(24 * time.Hour), nil
}
