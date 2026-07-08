package crawler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// registerTestRule points the per-host registry at an httptest server's host
// (which carries a random port) for the duration of a test.
func registerTestRule(t *testing.T, serverURL string, r siteRule) {
	t.Helper()
	u, err := url.Parse(serverURL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	siteRules[u.Host] = r
	t.Cleanup(func() { delete(siteRules, u.Host) })
}

const sampleArticleHTML = `<!doctype html><html><head><title>ignored</title></head>
<body>
  <h1 class="title-detail">Cảnh báo lừa đảo đầu tư forex</h1>
  <article class="fck_detail">
    <p class="Normal">Đối tượng mời gọi đầu tư sàn forex lợi nhuận cao.</p>
    <p class="Normal">  </p>
    <p class="Normal">Nạn nhân nạp tiền rồi bị chiếm đoạt.</p>
  </article>
</body></html>`

func TestFetch_ExtractsTitleAndContent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(sampleArticleHTML))
	}))
	defer srv.Close()
	registerTestRule(t, srv.URL, siteRule{source: "vnexpress", titleSel: "h1.title-detail", contentSel: "article.fck_detail"})

	doc, err := New().Fetch(context.Background(), srv.URL+"/canh-bao")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if doc.Title != "Cảnh báo lừa đảo đầu tư forex" {
		t.Errorf("title = %q", doc.Title)
	}
	if doc.Source != "vnexpress" {
		t.Errorf("source = %q", doc.Source)
	}
	// Blank paragraph dropped; two real paragraphs joined by a blank line.
	want := "Đối tượng mời gọi đầu tư sàn forex lợi nhuận cao.\n\nNạn nhân nạp tiền rồi bị chiếm đoạt."
	if doc.Content != want {
		t.Errorf("content = %q, want %q", doc.Content, want)
	}
	if !strings.Contains(gotUA, "ChamLaiBot") {
		t.Errorf("user-agent not sent, got %q", gotUA)
	}
}

func TestFetch_UnknownHost(t *testing.T) {
	// ruleFor fails before any network call, so an unregistered host is safe.
	_, err := New().Fetch(context.Background(), "https://unknown.example/article")
	if err == nil || !strings.Contains(err.Error(), "unknown host") {
		t.Fatalf("want unknown-host error, got %v", err)
	}
}

func TestFetch_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	registerTestRule(t, srv.URL, siteRule{source: "x", titleSel: "h1", contentSel: "article"})

	_, err := New().Fetch(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("want status-500 error, got %v", err)
	}
}

func TestFetch_EmptyContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>t</h1><div class="other">x</div></body></html>`))
	}))
	defer srv.Close()
	registerTestRule(t, srv.URL, siteRule{source: "x", titleSel: "h1", contentSel: "article.fck_detail"})

	_, err := New().Fetch(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "empty content") {
		t.Fatalf("want empty-content error, got %v", err)
	}
}

func TestFetch_RedirectToUnregisteredHostBlocked(t *testing.T) {
	// A host not in the allowlist, reachable only via a redirect FROM an
	// allowlisted host — the SSRF gap defaultCheckRedirect closes.
	unregistered := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(sampleArticleHTML))
	}))
	defer unregistered.Close()

	allowlisted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, unregistered.URL+"/x", http.StatusFound)
	}))
	defer allowlisted.Close()
	registerTestRule(t, allowlisted.URL, siteRule{source: "x", titleSel: "h1", contentSel: "article"})

	_, err := New().Fetch(context.Background(), allowlisted.URL)
	if err == nil || !strings.Contains(err.Error(), "redirect to unregistered host") {
		t.Fatalf("want redirect-blocked error, got %v", err)
	}
}

func TestFetch_ResponseBodyIsCapped(t *testing.T) {
	const realContent = "nội dung thật, không nên xuất hiện trong kết quả"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html><body><h1>t</h1><article class="fck_detail">`))
		// Pad well past maxResponseBytes before the real content, so a
		// correctly-capped read never reaches it. The container falls back
		// to its own text when no <p> is found (extractContent), so this
		// doesn't produce an "empty content" error — it's still non-empty
		// (garbage) content. What must never happen is realContent, planted
		// after the padding, showing up in the result.
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+1024)))
		_, _ = w.Write([]byte(`<p class="Normal">` + realContent + `</p></article></body></html>`))
	}))
	defer srv.Close()
	registerTestRule(t, srv.URL, siteRule{source: "x", titleSel: "h1", contentSel: "article.fck_detail"})

	doc, err := New().Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v (expected success on truncated-but-non-empty content)", err)
	}
	if strings.Contains(doc.Content, realContent) {
		t.Error("content past maxResponseBytes was not truncated — size cap is not effective")
	}
}

func TestInferScamType(t *testing.T) {
	cases := []struct {
		name, title, content, want string
	}{
		// original 5 types
		{"vacation", "Lừa đảo hợp đồng kỳ nghỉ resort", "", ScamVacationContract},
		{"job", "Tuyển cộng tác viên online chốt đơn", "việc nhẹ lương cao", ScamFakeJob},
		{"authority", "Giả mạo công an gọi điện", "viện kiểm sát", ScamImpersonationAuthority},
		{"investment", "Mời đầu tư forex", "lợi nhuận cao", ScamInvestmentFraud},
		{"romance", "Bẫy tình cảm qua mạng", "người nước ngoài gửi quà", ScamRomance},
		{"other", "Tin tức thời tiết hôm nay", "trời mưa", ScamOther},
		{"case-insensitive", "ĐẦU TƯ CRYPTO", "", ScamInvestmentFraud},
		// 7 new types
		{"impersonation-service-sim", "Lừa đảo nâng cấp SIM 4G để chiếm tài khoản", "nhà mạng gọi điện", ScamImpersonationService},
		{"impersonation-service-health", "Mạo danh nhân viên y tế báo người thân cấp cứu", "chuyển tiền vào viện ngay", ScamImpersonationService},
		{"tech-deepfake", "Deepfake giả mạo giám đốc yêu cầu chuyển tiền", "công nghệ cao tạo video giả", ScamTechFraud},
		{"tech-malware", "Cảnh báo mã độc ẩn trong app xem phim miễn phí", "phần mềm độc hại đánh cắp ngân hàng", ScamTechFraud},
		{"tech-hack", "Hack tài khoản Zalo giả vay tiền người thân", "", ScamTechFraud},
		{"recovery", "Dịch vụ hỗ trợ lấy lại tiền bị lừa đảo", "chuyên thu hồi tiền", ScamRecovery},
		{"loan", "Vay tiền online qua app lãi suất thấp", "tín dụng đen không cần thế chấp", ScamLoan},
		{"loan-card", "Mở thẻ tín dụng nhanh không cần chứng minh thu nhập", "", ScamLoan},
		{"ecommerce-deposit", "Mua hàng online đặt cọc xong người bán biến mất", "bán hàng online lừa đảo", ScamEcommerce},
		{"ecommerce-tour", "Lừa đảo bán tour du lịch vé máy bay giá rẻ", "combo du lịch 0 đồng", ScamEcommerce},
		{"package-delivery", "Giả shipper giao hàng yêu cầu chuyển khoản trước", "bưu kiện không nhận", ScamPackageDelivery},
		{"prize-gift", "Thông báo trúng thưởng xe máy tri ân khách hàng", "nhận quà miễn phí", ScamPrizeGift},
		{"prize-gift-tặng", "Chương trình tặng quà miễn phí nhân dịp lễ", "", ScamPrizeGift},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := InferScamType(c.title, c.content); got != c.want {
				t.Errorf("InferScamType(%q,%q) = %q, want %q", c.title, c.content, got, c.want)
			}
		})
	}
}

func TestLoadURLs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "seeds.txt")
	content := "# a comment\n\nhttps://vnexpress.net/a\n  https://tuoitre.vn/b  \n# another\nhttps://vtv.vn/c\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	urls, err := LoadURLs(path)
	if err != nil {
		t.Fatalf("LoadURLs: %v", err)
	}
	want := []string{"https://vnexpress.net/a", "https://tuoitre.vn/b", "https://vtv.vn/c"}
	if len(urls) != len(want) {
		t.Fatalf("got %v, want %v", urls, want)
	}
	for i := range want {
		if urls[i] != want[i] {
			t.Errorf("urls[%d] = %q, want %q", i, urls[i], want[i])
		}
	}
}
