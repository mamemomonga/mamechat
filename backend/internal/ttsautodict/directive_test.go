package ttsautodict

import "testing"

func TestExtractDirectives(t *testing.T) {
	got := ExtractDirectives("[Claude](クロード) と [Nothing Phone](ナッシングフォン) と [🐙](タコ)")
	if len(got) != 3 {
		t.Fatalf("len(ExtractDirectives) = %d, want 3", len(got))
	}
	if got[0].Term != "Claude" || got[0].Reading != "クロード" {
		t.Fatalf("first directive = %#v", got[0])
	}
	// スペースを含む単語に対応する。
	if got[1].Term != "Nothing Phone" || got[1].Reading != "ナッシングフォン" {
		t.Fatalf("second directive = %#v", got[1])
	}
	// 絵文字も登録できる。
	if got[2].Term != "🐙" || got[2].Reading != "タコ" {
		t.Fatalf("third directive = %#v", got[2])
	}
}

func TestExtractDirectivesIgnoresNonKatakanaReading(t *testing.T) {
	got := ExtractDirectives("[Claude](cloud flare)")
	if len(got) != 0 {
		t.Fatalf("ExtractDirectives returned %#v, want empty", got)
	}
}

func TestExtractDirectivesIgnoresBareParens(t *testing.T) {
	// 旧構文(角括弧なし)は意図しない登録を避けるため検出しない。
	got := ExtractDirectives("CloudFlare(クラウドフレア) 今日（キョウ）")
	if len(got) != 0 {
		t.Fatalf("ExtractDirectives returned %#v, want empty", got)
	}
}

func TestReplaceDirectives(t *testing.T) {
	got := ReplaceDirectives("[Claude](クロード) を使う")
	want := "クロード を使う"
	if got != want {
		t.Fatalf("ReplaceDirectives() = %q, want %q", got, want)
	}
}
