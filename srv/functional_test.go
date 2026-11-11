package srv

import (
	"net/url"
	"strings"
	"testing"
)

func TestInjectIntoHead(t *testing.T) {
	const inj = "<style>a[href$='.pdf']{color:red} $1 ${1}</style>"

	page := `<html><head lang="en"><title>x</title></head><body><header class="top">h</header><header>h2</header></body></html>`
	out := injectIntoHead(page, inj)
	if n := strings.Count(out, inj); n != 1 {
		t.Fatalf("injection appears %d times, want 1\n%s", n, out)
	}
	if !strings.Contains(out, `<head lang="en">`+inj+`<title>`) {
		t.Errorf("injection not placed right after <head>:\n%s", out)
	}

	// No <head>: a <header> must not be mistaken for it.
	out = injectIntoHead(`<html><body><header>h</header></body></html>`, inj)
	if !strings.HasPrefix(out, "<html><head>"+inj+"</head>") {
		t.Errorf("expected a head to be created after <html>:\n%s", out)
	}

	// Neither tag: prepend.
	if out := injectIntoHead("plain", inj); out != inj+"plain" {
		t.Errorf("expected injection to be prepended: %q", out)
	}
}

func TestRewriteRelativeURLs(t *testing.T) {
	base, _ := url.Parse("https://site.example/dir/page")
	in := `<img src="/a.png"><script src="//cdn.example/x.js"></script><a href='/b'>b</a><a href="https://o.example/c">c</a>`
	want := `<img src="https://site.example/a.png"><script src="//cdn.example/x.js"></script><a href='https://site.example/b'>b</a><a href="https://o.example/c">c</a>`
	if got := rewriteRelativeURLs(in, base); got != want {
		t.Errorf("rewriteRelativeURLs:\n got %s\nwant %s", got, want)
	}
}
