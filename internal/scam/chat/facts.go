package chat

// Static, controlled facts the answerer is grounded on for the project/founder
// flows. Content is trusted (goes into the system prompt); it is NOT derived
// from untrusted user input. These are starter placeholders — the project
// owner should refine the exact wording.
const (
	projectFacts = `Thông tin về dự án ChậmLại.vn:
- Là một dự án phi lợi nhuận, mã nguồn mở.
- Mục tiêu: giúp người dùng Việt Nam phát hiện các kịch bản/tin nhắn lừa đảo trực tuyến.
- Không thu thập dữ liệu cá nhân của người dùng.
- Cách hoạt động: người dùng gửi một tin nhắn đáng ngờ, hệ thống so khớp với kho mẫu lừa đảo đã biết và chấm mức độ rủi ro (đỏ/vàng/xanh).
- Được tạo ra bởi Nguyễn Văn Biên.`

	founderFacts = `Thông tin về người sáng lập:
- Tên: Nguyễn Văn Biên.
- Có khoảng 5 năm kinh nghiệm phát triển phần mềm.
- Chuyên môn: Go backend, xây dựng ứng dụng AI (AI Application), và phát triển mobile với Flutter.
- Học tại Đại học Bách Khoa TP.HCM.
- Là người khởi tạo dự án ChậmLại.vn.`
)
