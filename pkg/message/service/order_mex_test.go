package message_service

import (
	"encoding/json"
	"testing"

	"go.mau.fi/whatsmeow/types"
)

// Shaped after the fields WhatsApp Web's WAWebBizQueryOrderJobQuery selects
// (ids, names and numbers are made up).
const sampleOrderMexData = `{
  "xwa_checkout_get_order_info": {
    "order": {
      "creation_time_stamp": "1790000000",
      "price_details": {"subtotal_amount": "5000", "currency": "USD", "total_amount": "5000"},
      "products": [
        {"id": "111", "name": "Stylo", "price": "2500", "currency": "USD", "quantity": "2",
         "media": {"images": [{"id": "img1", "request_image_url": "https://example.test/i.jpg"}]},
         "variant_info": {"variant_properties": [{"name": "Couleur", "value": "Bleu"}]}},
        {"id": "222", "name": "Cahier", "price": 100, "currency": "USD", "quantity": 1}
      ]
    }
  }
}`

func TestParseOrderMexData(t *testing.T) {
	details, err := parseOrderMexData("9001", json.RawMessage(sampleOrderMexData))
	if err != nil {
		t.Fatal(err)
	}
	if details.ID != "9001" || len(details.Products) != 2 {
		t.Fatalf("unexpected details: %+v", details)
	}
	p := details.Products[0]
	if p.ID != "111" || p.Name != "Stylo" || p.Quantity != 2 || p.Price != 2500 || p.Currency != "USD" {
		t.Fatalf("unexpected first product: %+v", p)
	}
	if p.ImageID != "img1" || p.ImageURL != "https://example.test/i.jpg" {
		t.Fatalf("image not mapped: %+v", p)
	}
	if p.VariantInfo.Properties != "Couleur:Bleu" {
		t.Fatalf("variant not mapped: %q", p.VariantInfo.Properties)
	}
	if details.Products[1].Price != 100 || details.Products[1].Quantity != 1 {
		t.Fatalf("number-typed fields not read: %+v", details.Products[1])
	}
	if details.Price.Total != 5000 || details.Price.Currency != "USD" || details.CreatedAt.Unix() != 1790000000 {
		t.Fatalf("totals/time not mapped: %+v", details)
	}
}

func TestParseOrderMexDataRejectsEmptyReplies(t *testing.T) {
	for name, raw := range map[string]string{
		"no order":    `{"xwa_checkout_get_order_info": {}}`,
		"null":        `{"xwa_checkout_get_order_info": null}`,
		"no products": `{"xwa_checkout_get_order_info": {"order": {"products": []}}}`,
		"not json":    `nope`,
	} {
		if _, err := parseOrderMexData("1", json.RawMessage(raw)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestOrderMexVariablesShape(t *testing.T) {
	b, _ := json.Marshal(orderMexVariables("123@lid", "9001", "tok"))
	want := `{"request":{"order":{"id":"9001","image_dimensions":{"height":100,"width":100},"jid":"123@lid","token":{"sensitive_string_value":"tok"}}}}`
	if string(b) != want {
		t.Fatalf("variables mismatch:\n got %s\nwant %s", b, want)
	}
}

func TestSellerJIDCandidatesOrderAndDedup(t *testing.T) {
	pn := types.NewADJID("243841363015", 0, 4)
	lid := types.NewJID("199042273427633", types.HiddenUserServer)
	got := sellerJIDCandidates("199042273427633@lid", pn, lid, types.EmptyJID)
	want := []string{"199042273427633@lid", "243841363015@s.whatsapp.net"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v want %v", got, want)
	}
	if only := sellerJIDCandidates("", types.EmptyJID); len(only) != 0 {
		t.Fatalf("expected none, got %v", only)
	}
}

func TestOrderMexQueryIDOverride(t *testing.T) {
	t.Setenv("WA_MEX_ORDER_QUERY_ID", "")
	if orderMexQueryID() != defaultOrderMexQueryID {
		t.Fatal("default not used")
	}
	t.Setenv("WA_MEX_ORDER_QUERY_ID", " 555 ")
	if orderMexQueryID() != "555" {
		t.Fatal("override not used")
	}
}
