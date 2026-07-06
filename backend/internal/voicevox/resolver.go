package voicevox

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type ResolvedSpeaker struct {
	Character Character
	StyleID   int32
	StyleName string
}

type Resolver struct {
	baseURL string
	client  *http.Client
	mu      sync.Mutex
	cache   map[string]ResolvedSpeaker
}

func NewResolver(baseURL string) *Resolver {
	return &Resolver{
		baseURL: baseURL,
		client:  &http.Client{Timeout: 5 * time.Second},
		cache:   map[string]ResolvedSpeaker{},
	}
}

func (r *Resolver) Resolve(ctx context.Context, uuid string) (ResolvedSpeaker, error) {
	character, ok := CharacterByUUID(uuid)
	if !ok {
		return ResolvedSpeaker{}, fmt.Errorf("unknown voicevox speaker: %s", uuid)
	}
	r.mu.Lock()
	if resolved, ok := r.cache[character.UUID]; ok {
		r.mu.Unlock()
		return resolved, nil
	}
	r.mu.Unlock()

	resolved, err := r.resolveFromEngine(ctx, character)
	if err != nil {
		return ResolvedSpeaker{}, err
	}
	r.mu.Lock()
	r.cache[character.UUID] = resolved
	r.mu.Unlock()
	return resolved, nil
}

func (r *Resolver) resolveFromEngine(ctx context.Context, character Character) (ResolvedSpeaker, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/speakers", nil)
	if err != nil {
		return ResolvedSpeaker{}, err
	}
	res, err := r.client.Do(req)
	if err != nil {
		return ResolvedSpeaker{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ResolvedSpeaker{}, fmt.Errorf("voicevox speakers status %d", res.StatusCode)
	}
	var speakers []struct {
		Name        string `json:"name"`
		SpeakerUUID string `json:"speaker_uuid"`
		Styles      []struct {
			ID   int32  `json:"id"`
			Name string `json:"name"`
		} `json:"styles"`
	}
	if err := json.NewDecoder(res.Body).Decode(&speakers); err != nil {
		return ResolvedSpeaker{}, err
	}
	for _, speaker := range speakers {
		if speaker.SpeakerUUID != character.UUID || len(speaker.Styles) == 0 {
			continue
		}
		return ResolvedSpeaker{
			Character: character,
			StyleID:   speaker.Styles[0].ID,
			StyleName: speaker.Styles[0].Name,
		}, nil
	}
	return ResolvedSpeaker{}, fmt.Errorf("voicevox speaker not found: %s", character.UUID)
}
