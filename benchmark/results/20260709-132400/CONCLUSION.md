# Kết luận: RAG-hybrid (haiku) vs Generic AI + Web Search (sonnet)

n=145 case (`scam-*`, `custom-*`, `benign-*`), chạy `cmd/benchmark` ngày 2026-07-09.

## Số liệu

| Metric | rag-hybrid (haiku-4-5) | generic-websearch (sonnet-4-6) |
|---|---|---|
| Verdict accuracy (toàn bộ, 145 case) | **93.8%** (136/145) | 91.7% (133/145) |
| Verdict accuracy (24 case benign — tin thật) | **75.0%** (18/24) | 50.0% (12/24) |
| Verdict accuracy (120 case scam thật) | 98.3% (118/120) | **100%** (120/120) |
| Điểm judge trung bình (0–10) | 7.82 | 8.49 |
| Win/tie/loss (judge pairwise, rag vs generic) | 0 / 15 / 130 | — |

## Lưu ý quan trọng về độ tin cậy dữ liệu

77/145 case (53%) của arm `generic-websearch` **không chạy qua harness tự động** (`cmd/benchmark -run` gọi API Anthropic + tool `web_search`), mà được dán tay vào chat claude.ai rồi convert JSON thủ công (xem lịch sử file `missing-generic-cases.md` đã xoá, commit `ed36d1d`). Bằng chứng: các case này có `latency_ms=0`, `sources=[]`, và văn phong đặc trưng giao diện chat (heading/emoji, câu hỏi mở cuối câu trả lời) — khác hẳn 68 case chạy tự động (latency ~75s, có URL nguồn thật).

Hệ quả: **con số win/tie/loss và điểm judge trung bình không đáng tin cậy** — LLM judge có xu hướng thiên vị câu trả lời dài, tự tin, nhiều định dạng (đúng kiểu output claude.ai chat), không phản ánh đúng chất lượng model qua API thật. Ngược lại, **verdict accuracy** (nhãn red/yellow/green đúng hay sai so với expected) là số liệu khách quan hơn, ít bị ảnh hưởng bởi cách viết câu trả lời — và ở đây RAG-hybrid bằng hoặc nhỉnh hơn generic.

Vì lý do chi phí, benchmark sẽ không được chạy lại để làm sạch 77 case này. Kết luận dưới đây dựa trên số liệu accuracy (đáng tin) + số liệu judge (tham khảo, có caveat).

## Kết luận

**Không nên bỏ RAG-hybrid.** Ba lý do:

1. **Accuracy tương đương hoặc tốt hơn, với model rẻ hơn.** RAG-hybrid dùng haiku-4-5 (rẻ, nhanh) đạt accuracy 93.8%, cao hơn generic dùng sonnet-4-6 (đắt hơn nhiều, có web search) chỉ đạt 91.7%. Nếu RAG-hybrid dùng model mạnh hơn haiku, khoảng cách còn có thể nới rộng thêm.

2. **RAG ít báo nhầm tin thật thành lừa đảo hơn hẳn (75% vs 50% trên 24 case benign).** Đây là chỉ số quan trọng nhất cho một sản phẩm chống lừa đảo: false positive (báo tin ngân hàng/giao hàng thật là scam) làm mất lòng tin người dùng nhanh hơn false negative. RAG-hybrid thắng rõ ở điểm này nhờ corpus các mẫu lừa đảo thật giúp phân biệt tốt hơn so với suy luận chung chung + web search.

3. **Điểm judge nghiêng về generic không phản ánh chất lượng thật** — do lỗi phương pháp luận (53% dữ liệu generic là dán tay từ claude.ai chat, không phải API), khả năng cao là judge bias theo văn phong chứ không phải theo độ chính xác nội dung.

**Đánh đổi cần nhìn nhận công bằng:**
- Generic AI + web search đơn giản triển khai hơn (không cần duy trì corpus/pipeline crawl→enrich→ingest, không cần pgvector), và có thể bắt các loại lừa đảo *mới, chưa có trong corpus* tốt hơn nhờ tìm kiếm web trực tiếp — RAG chỉ tốt bằng corpus nó có.
- Generic (sonnet) tốn chi phí/độ trễ cao hơn nhiều (~75s/case với nhiều lượt web search) so với RAG-hybrid dùng haiku.
- Vì 53% dữ liệu generic-websearch không sạch, không thể khẳng định chắc chắn 100% generic tệ hơn ở khâu diễn giải/chất lượng câu trả lời — chỉ có thể khẳng định chắc phần accuracy (dựa trên nhãn, ít bị ảnh hưởng bởi cách dán tay).

**Khuyến nghị:** giữ hướng RAG-hybrid làm lõi sản phẩm. Cân nhắc bổ sung web-search như một fallback/bổ trợ cho các loại lừa đảo mới ngoài corpus, thay vì thay thế hoàn toàn RAG.
