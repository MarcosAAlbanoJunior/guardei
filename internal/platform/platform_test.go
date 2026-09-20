package platform

import "testing"

func TestCanonical(t *testing.T) {
	cases := []struct{ in, plat, want string }{
		{"https://youtu.be/abc123?si=xyz", YouTube, "https://youtube.com/watch?v=abc123"},
		{"https://www.youtube.com/watch?v=abc123&utm_source=x&t=10", YouTube, "https://youtube.com/watch?v=abc123"},
		{"https://m.youtube.com/shorts/abc123", YouTube, "https://youtube.com/watch?v=abc123"},
		{"https://twitter.com/user/status/1?s=20", X, "https://x.com/user/status/1"},
		{"https://www.instagram.com/reel/AbC/?igshid=zz", Instagram, "https://instagram.com/reel/AbC"},
		{"https://www.tiktok.com/@u/video/1?is_from_webapp=1", TikTok, "https://tiktok.com/@u/video/1"},
		{"https://example.com/a/?utm_medium=x&id=3", Other, "https://example.com/a?id=3"},
	}
	for _, c := range cases {
		plat, got, err := Canonical(c.in)
		if err != nil || plat != c.plat || got != c.want {
			t.Errorf("Canonical(%q) = %q, %q, %v; quer %q, %q", c.in, plat, got, err, c.plat, c.want)
		}
	}
	if _, _, err := Canonical("ftp://x.com/a"); err == nil {
		t.Error("esperava erro para esquema ftp")
	}
}

func TestFindURL(t *testing.T) {
	link, rest, ok := FindURL("olha isso https://youtu.be/abc, receita de bolo")
	if !ok || link != "https://youtu.be/abc" || rest != "olha isso , receita de bolo" {
		t.Errorf("got %q %q %v", link, rest, ok)
	}
	if _, rest, ok := FindURL("receita de bolo"); ok || rest != "receita de bolo" {
		t.Errorf("sem link: %q %v", rest, ok)
	}
}
