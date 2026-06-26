---
title: Cảnh báo chiêu trò lừa đảo đầu tư tiền ảo
scam_type: investment_fraud
source: youtube
url: https://www.youtube.com/watch?v=EXAMPLE
---
Đây là nội dung transcript được xuất thủ công (ví dụ từ Gemini đọc video YouTube,
hoặc copy tay từ một nguồn không crawl được).

Đặt file này vào cmd/crawler/data/ với phần mở rộng .md. Crawler sẽ đọc frontmatter
để lấy url/title/source/scam_type và phần thân làm nội dung để index.

Nếu bỏ trống "scam_type", crawler sẽ tự suy ra bằng rule-based InferScamType.
Trường "url" là bắt buộc (dùng để chống trùng); "source" thiếu thì mặc định "manual".
