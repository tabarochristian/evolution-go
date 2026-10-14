package message_service

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mau.fi/whatsmeow/types"
)

// WhatsApp retired the legacy "fb:thrift_iq" order query that whatsmeow's
// GetOrderDetails sends (WhatsApp Web's own code now lists it under
// WAWapDeprecatedSmaxID): every call answers "501 feature-not-implemented".
// WhatsApp Web asks for an order with the persisted GraphQL query
// WAWebBizQueryOrderJobQuery instead, carried as MEX ("w:mex") over the same
// authenticated websocket the gateway already holds as a linked device - the
// same transport whatsmeow uses for newsletters. Unlike the old query it
// takes the seller's JID.
//
// The persisted query id changes whenever WhatsApp changes the query, so it
// can be overridden from the environment without a rebuild.
const defaultOrderMexQueryID = "26593811266898374"

func orderMexQueryID() string {
	if v := strings.TrimSpace(os.Getenv("WA_MEX_ORDER_QUERY_ID")); v != "" {
		return v
	}
	return defaultOrderMexQueryID
}

func orderMexVariables(sellerJID, orderID, token string) map[string]any {
	return map[string]any{
		"request": map[string]any{
			"order": map[string]any{
				"jid":              sellerJID,
				"token":            map[string]string{"sensitive_string_value": token},
				"id":               orderID,
				"image_dimensions": map[string]int{"height": 100, "width": 100},
			},
		},
	}
}

// flexInt reads a GraphQL number that arrives either as a JSON number or as a
// decimal string ("" or null meaning "not provided").
type flexInt int64

func (f *flexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.TrimSpace(string(b)), `"`)
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return fmt.Errorf("not an integer: %q", s)
	}
	*f = flexInt(n)
	return nil
}

type orderMexData struct {
	Info *struct {
		Order *struct {
			CreationTimeStamp flexInt `json:"creation_time_stamp"`
			PriceDetails      *struct {
				SubtotalAmount flexInt `json:"subtotal_amount"`
				TotalAmount    flexInt `json:"total_amount"`
				Currency       string  `json:"currency"`
			} `json:"price_details"`
			Products []struct {
				ID          string  `json:"id"`
				Name        string  `json:"name"`
				Price       flexInt `json:"price"`
				Currency    string  `json:"currency"`
				Quantity    flexInt `json:"quantity"`
				VariantInfo *struct {
					VariantProperties []struct {
						Name  string `json:"name"`
						Value string `json:"value"`
					} `json:"variant_properties"`
				} `json:"variant_info"`
				Media *struct {
					Images []struct {
						ID              string `json:"id"`
						RequestImageURL string `json:"request_image_url"`
					} `json:"images"`
				} `json:"media"`
			} `json:"products"`
		} `json:"order"`
	} `json:"xwa_checkout_get_order_info"`
}

// parseOrderMexData turns the MEX reply into the same structure the legacy
// query returned, so the HTTP response (and Django behind it) can't tell
// which path answered.
func parseOrderMexData(orderID string, raw json.RawMessage) (*types.OrderDetails, error) {
	var data orderMexData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("order mex reply is not valid: %w", err)
	}
	if data.Info == nil || data.Info.Order == nil {
		return nil, fmt.Errorf("order mex reply had no order")
	}

	order := data.Info.Order
	details := &types.OrderDetails{ID: orderID}
	if order.CreationTimeStamp > 0 {
		details.CreatedAt = time.Unix(int64(order.CreationTimeStamp), 0)
	}
	if pd := order.PriceDetails; pd != nil {
		details.Price = types.OrderPrice{
			Subtotal: int64(pd.SubtotalAmount),
			Total:    int64(pd.TotalAmount),
			Currency: pd.Currency,
		}
	}
	for _, p := range order.Products {
		product := types.OrderProduct{
			ID:       p.ID,
			Name:     p.Name,
			Price:    int64(p.Price),
			Currency: p.Currency,
			Quantity: int(p.Quantity),
		}
		if p.Media != nil && len(p.Media.Images) > 0 {
			product.ImageID = p.Media.Images[0].ID
			product.ImageURL = p.Media.Images[0].RequestImageURL
		}
		if p.VariantInfo != nil {
			var props []string
			for _, vp := range p.VariantInfo.VariantProperties {
				props = append(props, vp.Name+":"+vp.Value)
			}
			product.VariantInfo.Properties = strings.Join(props, ",")
		}
		details.Products = append(details.Products, product)
	}
	if len(details.Products) == 0 {
		return nil, fmt.Errorf("order mex reply had no products")
	}
	return details, nil
}

// sellerJIDCandidates lists the JIDs to try as the order's seller, most
// likely first and without duplicates: the one carried by the order message,
// then this account's own phone-number and LID forms (the order is placed
// with the business we are logged in as).
func sellerJIDCandidates(fromMessage string, own ...types.JID) []string {
	seen := map[string]bool{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	add(fromMessage)
	for _, jid := range own {
		if !jid.IsEmpty() {
			add(jid.ToNonAD().String())
		}
	}
	return out
}
