// vapid-keygen は Web Push 用の VAPID 鍵ペアを生成し、設定すべき環境変数を標準出力へ出す。
// backend コンテナで単発起動して使う想定：
//
//	docker compose run --rm backend /vapid-keygen
//
// 出力された VAPID_PUBLIC_KEY / VAPID_PRIVATE_KEY / VAPID_SUBJECT を .env などに設定する。
package main

import (
	"fmt"
	"os"

	"github.com/mamemomonga/mamechat/backend/internal/webpush"
)

func main() {
	pub, priv, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to generate VAPID keys:", err)
		os.Exit(1)
	}
	fmt.Println("# Web Push (VAPID) の設定。以下を環境変数に設定してください。")
	fmt.Println("# VAPID_SUBJECT は連絡先（mailto: もしくは https:// のURL）に置き換えてください。")
	fmt.Printf("VAPID_PUBLIC_KEY=%s\n", pub)
	fmt.Printf("VAPID_PRIVATE_KEY=%s\n", priv)
	fmt.Printf("VAPID_SUBJECT=%s\n", "mailto:admin@example.com")
}
