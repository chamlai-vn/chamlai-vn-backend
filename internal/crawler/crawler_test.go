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

func TestInferScamType(t *testing.T) {
	cases := []struct {
		name, title, content, want string
	}{
		{"vacation", "Lừa đảo hợp đồng kỳ nghỉ resort", "", ScamVacationContract},
		{"job", "Tuyển cộng tác viên online chốt đơn", "việc nhẹ lương cao", ScamFakeJob},
		{"authority", "Giả mạo công an gọi điện", "viện kiểm sát", ScamImpersonationAuthority},
		{"investment", "Mời đầu tư forex", "lợi nhuận cao", ScamInvestmentFraud},
		{"romance", "Bẫy tình cảm qua mạng", "người nước ngoài gửi quà", ScamRomance},
		{"other", "Tin tức thời tiết hôm nay", "trời mưa", ScamOther},
		{"case-insensitive", "ĐẦU TƯ CRYPTO", "", ScamInvestmentFraud},
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

func TestParseLocalFile(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("valid", func(t *testing.T) {
		p := write("ok.md", "---\ntitle: Cảnh báo X\nscam_type: investment_fraud\nsource: youtube\nurl: https://youtu.be/abc\n---\nNội dung transcript dài.\n")
		doc, scamType, err := ParseLocalFile(p)
		if err != nil {
			t.Fatalf("ParseLocalFile: %v", err)
		}
		if doc.URL != "https://youtu.be/abc" || doc.Title != "Cảnh báo X" || doc.Source != "youtube" {
			t.Errorf("doc = %+v", doc)
		}
		if doc.Content != "Nội dung transcript dài." {
			t.Errorf("content = %q", doc.Content)
		}
		if scamType != "investment_fraud" {
			t.Errorf("scamType = %q", scamType)
		}
	})

	t.Run("source defaults to manual", func(t *testing.T) {
		p := write("nosrc.md", "---\nurl: https://x.test/y\n---\nbody\n")
		doc, _, err := ParseLocalFile(p)
		if err != nil {
			t.Fatalf("ParseLocalFile: %v", err)
		}
		if doc.Source != defaultFileSource {
			t.Errorf("source = %q, want %q", doc.Source, defaultFileSource)
		}
	})

	t.Run("missing url", func(t *testing.T) {
		p := write("nourl.md", "---\ntitle: x\n---\nbody\n")
		if _, _, err := ParseLocalFile(p); err == nil || !strings.Contains(err.Error(), "url") {
			t.Fatalf("want missing-url error, got %v", err)
		}
	})

	t.Run("no frontmatter fence", func(t *testing.T) {
		p := write("nofm.md", "just body, no fence\n")
		if _, _, err := ParseLocalFile(p); err == nil || !strings.Contains(err.Error(), "fence") {
			t.Fatalf("want fence error, got %v", err)
		}
	})

	t.Run("unterminated frontmatter", func(t *testing.T) {
		p := write("unterm.md", "---\nurl: https://x.test/y\nbody without closing fence\n")
		if _, _, err := ParseLocalFile(p); err == nil || !strings.Contains(err.Error(), "unterminated") {
			t.Fatalf("want unterminated error, got %v", err)
		}
	})

	t.Run("empty body", func(t *testing.T) {
		p := write("empty.md", "---\nurl: https://x.test/y\n---\n   \n")
		if _, _, err := ParseLocalFile(p); err == nil || !strings.Contains(err.Error(), "empty body") {
			t.Fatalf("want empty-body error, got %v", err)
		}
	})
}
