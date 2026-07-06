package voicevox

import (
	"crypto/rand"
	"math/big"
)

type Character struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
	URL  string `json:"url"`
}

var Characters = []Character{
	{Name: "四国めたん", UUID: "7ffcb7ce-00ec-4bdc-82cd-45a8889e43ff", URL: "https://zunko.jp/con_ongen_kiyaku.html"},
	{Name: "ずんだもん", UUID: "388f246b-8c41-4ac1-8e2d-5d79f3ff56d9", URL: "https://zunko.jp/con_ongen_kiyaku.html"},
	{Name: "春日部つむぎ", UUID: "35b2c544-660e-401e-b503-0e14c635303a", URL: "https://tsumugi-official.studio.site/rule"},
	{Name: "波音リツ", UUID: "b1a81618-b27b-40d2-b0ea-27a9ad408c4b", URL: "http://canon-voice.com/kiyaku.html"},
	{Name: "玄野武宏", UUID: "c30dc15a-0992-4f8d-8bb8-ad3b314e6a6f", URL: "https://virvoxproject.wixsite.com/official/voicevoxの利用規約"},
	{Name: "白上虎太郎", UUID: "e5020595-5c5d-4e87-b849-270a518d0dcf", URL: "https://virvoxproject.wixsite.com/official/voicevoxの利用規約"},
	{Name: "冥鳴ひまり", UUID: "8eaad775-3119-417e-8cf4-2a10bfd592c8", URL: "https://meimeihimari.wixsite.com/himari/terms-of-use"},
	{Name: "九州そら", UUID: "481fb609-6446-4870-9f46-90c4dd623403", URL: "https://zunko.jp/con_ongen_kiyaku.html"},
	{Name: "剣崎雌雄", UUID: "1a17ca16-7ee5-4ea5-b191-2f02ace24d21", URL: "https://frontier.creatia.cc/fanclubs/413/posts/4507"},
	{Name: "WhiteCUL", UUID: "67d5d8da-acd7-4207-bb10-b5542d3a663b", URL: "https://www.whitecul.com/guideline"},
	{Name: "ちび式じい", UUID: "468b8e94-9da4-4f7a-8715-a22a48844f9e", URL: "https://docs.google.com/presentation/d/1AcD8zXkfzKFf2ertHwWRwJuQXjNnijMxhz7AJzEkaI4"},
	{Name: "櫻歌ミコ", UUID: "0693554c-338e-4790-8982-b9c6d476dc69", URL: "https://voicevox35miko.studio.site/rule"},
	{Name: "小夜/SAYO", UUID: "a8cc6d22-aad0-4ab8-bf1e-2f843924164a", URL: "https://316soramegu.wixsite.com/sayo-official/guideline"},
	{Name: "ナースロボ＿タイプＴ", UUID: "882a636f-3bac-431a-966d-c5e6bba9f949", URL: "https://www.krnr.top/rules"},
	{Name: "†聖騎士 紅桜†", UUID: "471e39d2-fb11-4c8c-8d89-4b322d2498e0", URL: "https://commons.nicovideo.jp/material/nc296132"},
	{Name: "雀松朱司", UUID: "0acebdee-a4a5-4e12-a695-e19609728e30", URL: "https://virvoxproject.wixsite.com/official/voicevoxの利用規約"},
	{Name: "麒ヶ島宗麟", UUID: "7d1e7ba7-f957-40e5-a3fc-da49f769ab65", URL: "https://virvoxproject.wixsite.com/official/voicevoxの利用規約"},
	{Name: "春歌ナナ", UUID: "ba5d2428-f7e0-4c20-ac41-9dd56e9178b4", URL: "https://nanahira.jp/haruka_nana/guideline.html"},
	{Name: "猫使アル", UUID: "00a5c10c-d3bd-459f-83fd-43180b521a44", URL: "https://nekotukarb.wixsite.com/nekonohako/利用規約"},
	{Name: "猫使ビィ", UUID: "c20a2254-0349-4470-9fc8-e5c0f8cf3404", URL: "https://nekotukarb.wixsite.com/nekonohako/利用規約"},
	{Name: "中国うさぎ", UUID: "1f18ffc3-47ea-4ce0-9829-0576d03a7ec8", URL: "https://zunko.jp/con_ongen_kiyaku.html"},
	{Name: "栗田まろん", UUID: "04dbd989-32d0-40b4-9e71-17c920f2a8a9", URL: "https://aivoice.jp/character/maron/"},
	{Name: "あいえるたん", UUID: "dda44ade-5f9c-4a3a-9d2c-2a976c7476d9", URL: "https://www.infiniteloop.co.jp/special/iltan/terms/"},
	{Name: "満別花丸", UUID: "287aa49f-e56b-4530-a469-855776c84a8d", URL: "https://100hanamaru.wixsite.com/manbetsu-hanamaru/rule"},
	{Name: "琴詠ニア", UUID: "97a4af4b-086e-4efd-b125-7ae2da85e697", URL: "https://commons.nicovideo.jp/works/nc315435"},
	{Name: "中部つるぎ", UUID: "4614a7de-9829-465d-9791-97eb8a5f9b86", URL: "https://zunko.jp/con_ongen_kiyaku.html"},
}

func CharacterByUUID(uuid string) (Character, bool) {
	for _, character := range Characters {
		if character.UUID == uuid {
			return character, true
		}
	}
	return Character{}, false
}

func DefaultCharacter() Character {
	return Characters[1]
}

func RandomCharacter() Character {
	if len(Characters) == 0 {
		return Character{}
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(Characters))))
	if err != nil {
		return DefaultCharacter()
	}
	return Characters[n.Int64()]
}
