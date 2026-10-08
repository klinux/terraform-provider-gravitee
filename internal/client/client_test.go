package client

import (
	"encoding/json"
	"testing"
)

// A Management API 3.15 devolve api/plan/application como objeto no POST e no
// GET de detalhe, e como string nua na listagem. O swagger tipa so as duas
// primeiras (schema `Subscription`); a lista e um `PagedResult` generico, sem
// schema de item. Estes casos sao os corpos reais observados contra
// a Gravitee APIM 3.15 instance.
func TestSubscriptionAceitaAsTresFormas(t *testing.T) {
	casos := []struct {
		nome    string
		corpo   string
		wantAPI string
		wantPln string
		wantApp string
	}{
		{
			nome:    "GET lista: tudo string (forma nao tipada no spec)",
			corpo:   `{"id":"s1","api":"api-1","plan":"plan-1","application":"app-1","status":"ACCEPTED"}`,
			wantAPI: "api-1", wantPln: "plan-1", wantApp: "app-1",
		},
		{
			nome:    "GET detalhe: api e plan objeto, application null (conforme o spec)",
			corpo:   `{"id":"s1","api":{"id":"api-1","name":"registrations"},"plan":{"id":"plan-1","security":"OAUTH2"},"application":null,"status":"ACCEPTED"}`,
			wantAPI: "api-1", wantPln: "plan-1", wantApp: "",
		},
		{
			nome:    "POST: api objeto (conforme o spec)",
			corpo:   `{"id":"s1","api":{"id":"api-1"},"plan":{"id":"plan-1"},"status":"ACCEPTED"}`,
			wantAPI: "api-1", wantPln: "plan-1", wantApp: "",
		},
		{
			nome:    "campos ausentes",
			corpo:   `{"id":"s1","status":"PENDING"}`,
			wantAPI: "", wantPln: "", wantApp: "",
		},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var s Subscription
			if err := json.Unmarshal([]byte(c.corpo), &s); err != nil {
				t.Fatalf("nao decodificou: %v", err)
			}
			if s.API.ID != c.wantAPI {
				t.Errorf("api: quis %q, veio %q", c.wantAPI, s.API.ID)
			}
			if s.Plan.ID != c.wantPln {
				t.Errorf("plan: quis %q, veio %q", c.wantPln, s.Plan.ID)
			}
			if s.Application.ID != c.wantApp {
				t.Errorf("application: quis %q, veio %q", c.wantApp, s.Application.ID)
			}
		})
	}
}

// Ref sempre serializa como uuid nu, qualquer que tenha sido a forma de entrada.
func TestRefSerializaComoString(t *testing.T) {
	var s Subscription
	if err := json.Unmarshal([]byte(`{"api":{"id":"api-1","name":"x"}}`), &s); err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(s.API)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `"api-1"` {
		t.Errorf("quis \"api-1\", veio %s", b)
	}
}

// DELETE de subscription e um close: o registro sobrevive com CLOSED. Read tem
// que tratar isso como ausente, senao destroy+apply nunca converge.
func TestSubscriptionGone(t *testing.T) {
	casos := map[string]bool{
		"ACCEPTED": false,
		"PENDING":  false,
		"PAUSED":   false,
		"CLOSED":   true,
		"REJECTED": true,
	}
	for status, want := range casos {
		if got := SubscriptionGone(&Subscription{Status: status}); got != want {
			t.Errorf("status %s: quis gone=%v, veio %v", status, want, got)
		}
	}
	if !SubscriptionGone(nil) {
		t.Error("nil deveria contar como ausente")
	}
}

func TestNotFoundSoPegaQuatrocentosEQuatro(t *testing.T) {
	if !NotFound(&Error{Status: 404}) {
		t.Error("404 deveria ser NotFound")
	}
	for _, s := range []int{400, 401, 403, 409, 500} {
		if NotFound(&Error{Status: s}) {
			t.Errorf("HTTP %d nao deveria ser NotFound", s)
		}
	}
	if NotFound(nil) {
		t.Error("nil nao e NotFound")
	}
}

// PareiaPlanos tem de carregar o `id` dos planos existentes, pareando por nome.
// Sem o id no corpo, o import trata o plano como novo e tenta apagar o antigo,
// levando as subscriptions. Provado contra o stage: o import exige o `id` da
// API no topo E o `id` de cada plano.
func TestPareiaPlanosCarregaIDs(t *testing.T) {
	desejado := APIRaw{
		"gravitee": "2.0.0",
		"plans": []any{
			map[string]any{"name": "Default", "security": "KEY_LESS"},
			map[string]any{"name": "ApiKey", "security": "API_KEY"},
		},
	}
	atual := APIRaw{
		"id": "api-1",
		"plans": []any{
			map[string]any{"name": "ApiKey", "id": "p-apikey", "security": "API_KEY"},
			map[string]any{"name": "Default", "id": "p-default", "security": "KEY_LESS"},
		},
	}
	out, descartados, err := PareiaPlanos(desejado, atual)
	if err != nil {
		t.Fatal(err)
	}
	if len(descartados) != 0 {
		t.Errorf("nao deveria descartar nada, veio %v", descartados)
	}
	quer := map[string]string{"Default": "p-default", "ApiKey": "p-apikey"}
	for _, p := range out["plans"].([]any) {
		m := p.(map[string]any)
		nome := m["name"].(string)
		if m["id"] != quer[nome] {
			t.Errorf("plano %s: quis id %q, veio %v", nome, quer[nome], m["id"])
		}
	}
	// o desejado original nao pode ser mutado
	for _, p := range desejado["plans"].([]any) {
		if _, temID := p.(map[string]any)["id"]; temID {
			t.Error("PareiaPlanos mutou o desejado")
		}
	}
}

// Plano que existe hoje e nao esta no desejado seria apagado pelo import, junto
// com as subscriptions. PareiaPlanos tem de denunciar.
func TestPareiaPlanosDenunciaDescarte(t *testing.T) {
	desejado := APIRaw{"plans": []any{map[string]any{"name": "Default"}}}
	atual := APIRaw{"plans": []any{
		map[string]any{"name": "Default", "id": "p-1"},
		map[string]any{"name": "ApiKey", "id": "p-2", "security": "API_KEY"},
	}}
	_, descartados, err := PareiaPlanos(desejado, atual)
	if err != nil {
		t.Fatal(err)
	}
	if len(descartados) != 1 || descartados[0].Nome != "ApiKey" || descartados[0].ID != "p-2" {
		t.Fatalf("esperava denunciar ApiKey/p-2, veio %v", descartados)
	}
}

// Dois planos com o mesmo nome tornam o pareamento ambiguo: melhor falhar que
// apagar o plano errado.
func TestPareiaPlanosRecusaNomeDuplicado(t *testing.T) {
	atual := APIRaw{"plans": []any{
		map[string]any{"name": "Default", "id": "p-1"},
		map[string]any{"name": "Default", "id": "p-2"},
	}}
	if _, _, err := PareiaPlanos(APIRaw{}, atual); err == nil {
		t.Error("esperava erro de nome duplicado")
	}
}

// O export devolve os planos em ordem propria; AlinhaPlanos reordena pela ordem
// declarada para a comparacao nao acusar divergencia falsa.
func TestAlinhaPlanos(t *testing.T) {
	desejado := APIRaw{"plans": []any{
		map[string]any{"name": "Default"},
		map[string]any{"name": "ApiKey"},
	}}
	aplicado := APIRaw{"plans": []any{
		map[string]any{"name": "ApiKey", "id": "p-2"},
		map[string]any{"name": "Default", "id": "p-1"},
	}}
	out, faltando := AlinhaPlanos(desejado, aplicado)
	if len(faltando) != 0 {
		t.Errorf("nao deveria faltar nada, veio %v", faltando)
	}
	nomes := []string{}
	for _, p := range out["plans"].([]any) {
		nomes = append(nomes, p.(map[string]any)["name"].(string))
	}
	if nomes[0] != "Default" || nomes[1] != "ApiKey" {
		t.Errorf("esperava [Default ApiKey], veio %v", nomes)
	}
}

// Plano declarado que nao existe no servidor tem de ser denunciado: e sinal de
// que o import nao criou o que foi pedido.
func TestAlinhaPlanosDenunciaFaltante(t *testing.T) {
	desejado := APIRaw{"plans": []any{map[string]any{"name": "Novo"}}}
	aplicado := APIRaw{"plans": []any{map[string]any{"name": "Default", "id": "p-1"}}}
	_, faltando := AlinhaPlanos(desejado, aplicado)
	if len(faltando) != 1 || faltando[0] != "Novo" {
		t.Fatalf("esperava denunciar Novo, veio %v", faltando)
	}
}

// O import zera o `order` dos planos. Quem declarou precisa ser avisado.
func TestOrdemDeclaradaIgnorada(t *testing.T) {
	desejado := APIRaw{"plans": []any{
		map[string]any{"name": "Default", "order": float64(0)},
		map[string]any{"name": "ApiKey", "order": float64(1)},
	}}
	aplicado := APIRaw{"plans": []any{
		map[string]any{"name": "Default", "order": float64(0)},
		map[string]any{"name": "ApiKey", "order": float64(0)},
	}}
	fora := OrdemDeclaradaIgnorada(desejado, aplicado)
	if len(fora) != 1 {
		t.Fatalf("esperava 1 divergencia de ordem, veio %v", fora)
	}
}

// LimpaCamposDoServidor remove o que o APIM atribui, inclusive o id de topo --
// que por isso tem de ser reposto no corpo do import pelo Update.
func TestLimpaCamposDoServidor(t *testing.T) {
	in := APIRaw{
		"name": "x", "id": "api-1", "primaryOwner": map[string]any{"id": "u"},
		"members": []any{}, "pages": []any{},
		"plans": []any{map[string]any{
			"name": "Default", "id": "p-1", "created_at": 1, "updated_at": 2,
			"api": "api-1", "order": float64(3), "security": "KEY_LESS",
		}},
	}
	out := LimpaCamposDoServidor(in)
	for _, k := range []string{"id", "primaryOwner", "members", "pages"} {
		if _, tem := out[k]; tem {
			t.Errorf("%s deveria ter sido removido do topo", k)
		}
	}
	pl := out["plans"].([]any)[0].(map[string]any)
	for _, k := range []string{"id", "created_at", "updated_at", "api", "order"} {
		if _, tem := pl[k]; tem {
			t.Errorf("plano: %s deveria ter sido removido", k)
		}
	}
	if pl["security"] != "KEY_LESS" || pl["name"] != "Default" {
		t.Error("campos declarados nao deveriam ter sido tocados")
	}
	// a entrada nao pode ser mutada
	if _, tem := in["id"]; !tem {
		t.Error("LimpaCamposDoServidor mutou a entrada")
	}
}
