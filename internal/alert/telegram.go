package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/xlaez/bot33/internal/chain"
	"github.com/xlaez/bot33/internal/classify"
	"github.com/xlaez/bot33/internal/wallet"
)

type Telegram struct {
	token  string
	chatID string
	client *http.Client
}

func NewTelegram(token, chatID string) *Telegram {
	return &Telegram{
		token:  strings.TrimSpace(token),
		chatID: strings.TrimSpace(chatID),
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (t *Telegram) Enabled() bool {
	return t.token != "" && t.chatID != ""
}

type Payload struct {
	Wallet     wallet.Record
	Event      *classify.Event
	Action     classify.Action
	Collection string
}

func (t *Telegram) SendText(ctx context.Context, msg string) error {
	if !t.Enabled() {
		return fmt.Errorf("telegram not configured")
	}
	body, _ := json.Marshal(map[string]any{
		"chat_id":                  t.chatID,
		"text":                     msg,
		"disable_web_page_preview": true,
	})
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", t.token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := t.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("telegram status %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (t *Telegram) Send(ctx context.Context, p Payload) error {
	tag := "[CURATED]"
	if p.Wallet.Source == wallet.SourceDiscovered {
		tag = fmt.Sprintf("[DISCOVERED score=%.0f]", p.Wallet.Score)
	}
	label := p.Wallet.Label
	if label == "" {
		label = p.Wallet.Address
	}
	tokenID := "?"
	if p.Event.TokenID != nil {
		tokenID = p.Event.TokenID.String()
	}
	coll := p.Collection
	if coll == "" {
		coll = p.Event.Collection.Hex()
	}
	msg := fmt.Sprintf(
		"%s %s %s NFT\ncollection: %s #%s\nstandard: %s\nwallet: %s\ntx: %s\nwallet link: %s",
		tag,
		label,
		string(p.Action),
		coll,
		tokenID,
		p.Event.Standard,
		p.Wallet.Address,
		chain.ExplorerTx(p.Event.TxHash.Hex()),
		chain.ExplorerAddress(p.Wallet.Address),
	)
	return t.SendText(ctx, msg)
}
