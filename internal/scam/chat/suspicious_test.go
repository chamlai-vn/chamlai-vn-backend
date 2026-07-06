package chat

import "testing"

func TestLooksSuspicious(t *testing.T) {
	suspicious := []string{
		"bấm vào http://example.com ngay",
		"truy cập www.taixiu.top nhé",
		"vào t.me/abcgroup để nhận",
		"chuyển vào số tài khoản 1234567890",
		"mã OTP của bạn là 483920",
		"nhập mã 8842 để xác nhận",
		"trúng thưởng 100 triệu đồng",
		"việc nhẹ lương cao, hoa hồng cao",
		"vui lòng nạp tiền để rút",
		"tài khoản của bạn sẽ bị khóa tài khoản",
	}
	for _, s := range suspicious {
		if !looksSuspicious(s) {
			t.Errorf("looksSuspicious(%q) = false, want true", s)
		}
	}

	clean := []string{
		"dự án này là gì vậy",
		"ai là người sáng lập",
		"chào bạn, hôm nay thế nào",
		"bạn có thể làm gì",
	}
	for _, s := range clean {
		if looksSuspicious(s) {
			t.Errorf("looksSuspicious(%q) = true, want false", s)
		}
	}
}
