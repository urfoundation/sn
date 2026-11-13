package demand

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

const (
	coinbaseProductBook = "https://api.coinbase.com/api/v3/brokerage/market/product_book?product_id=TAO-USD&limit=1"
	marketResponseBytes = 1 << 20
)

// CoinbaseMarket reads Coinbase's public TAO-USD book, which takes no key. It is
// the price reference a swap is guarded against.
type CoinbaseMarket struct {
	url  string
	http *http.Client
}

// NewCoinbaseMarket builds the public market client.
func NewCoinbaseMarket() *CoinbaseMarket {
	return &CoinbaseMarket{url: coinbaseProductBook, http: &http.Client{Timeout: 15 * time.Second}}
}

// BestAsk is the lowest public ask in USD per TAO.
func (m *CoinbaseMarket) BestAsk(ctx context.Context) (float64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.url, nil)
	if err != nil {
		return 0, fmt.Errorf("demand: market: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := m.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("demand: market: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, marketResponseBytes))
	if err != nil {
		return 0, fmt.Errorf("demand: market: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("demand: market: HTTP %d", resp.StatusCode)
	}
	var book struct {
		Pricebook struct {
			Asks []struct {
				Price string `json:"price"`
			} `json:"asks"`
		} `json:"pricebook"`
	}
	if err := json.Unmarshal(body, &book); err != nil {
		return 0, fmt.Errorf("demand: market: decode: %w", err)
	}
	if len(book.Pricebook.Asks) == 0 {
		return 0, fmt.Errorf("demand: market: the TAO-USD book has no asks")
	}
	ask, err := strconv.ParseFloat(book.Pricebook.Asks[0].Price, 64)
	if err != nil || ask <= 0 {
		return 0, fmt.Errorf("demand: market: bad ask %q", book.Pricebook.Asks[0].Price)
	}
	return ask, nil
}
