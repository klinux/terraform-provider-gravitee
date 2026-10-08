// Package client fala com a Management API do Gravitee APIM 3.x.
//
// O provider e deliberadamente fino: ele nao entende policy nenhuma. A unica
// inteligencia que mora aqui e o tratamento das assimetrias da API 3.x que nao
// aparecem no swagger.json e que ja nos morderam na mao:
//
//   - DELETE de subscription significa "close", nao remove: o registro continua
//     existindo com status CLOSED. Ver SubscriptionGone.
//   - PUT de application exige o corpo inteiro (name, description e settings sao
//     obrigatorios). Update parcial derruba settings.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	base  string // .../management/organizations/{org}/environments/{env}
	token string
	http  *http.Client
}

func New(endpoint, org, env, token string, timeout time.Duration) *Client {
	return &Client{
		base: fmt.Sprintf("%s/organizations/%s/environments/%s",
			strings.TrimRight(endpoint, "/"), url.PathEscape(org), url.PathEscape(env)),
		token: token,
		http:  &http.Client{Timeout: timeout},
	}
}

// Error carrega o status e o corpo, porque a Management API costuma explicar a
// recusa no corpo e nao no status.
type Error struct {
	Method string
	Path   string
	Status int
	Body   string
}

func (e *Error) Error() string {
	b := e.Body
	if len(b) > 600 {
		b = b[:600] + "..."
	}
	return fmt.Sprintf("%s %s devolveu HTTP %d: %s", e.Method, e.Path, e.Status, b)
}

// NotFound distingue "nao existe" de "deu erro", que e o que o Read de cada
// recurso precisa para tirar o objeto do state em vez de falhar o apply.
func NotFound(err error) bool {
	var e *Error
	if ok := asErr(err, &e); ok {
		return e.Status == http.StatusNotFound
	}
	return false
}

func asErr(err error, target **Error) bool {
	for err != nil {
		if e, ok := err.(*Error); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func (c *Client) do(ctx context.Context, method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		raw, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("serializando corpo de %s %s: %w", method, path, err)
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &Error{Method: method, Path: path, Status: resp.StatusCode, Body: string(raw)}
	}
	if out == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decodificando resposta de %s %s: %w", method, path, err)
	}
	return nil
}

// ---------- application ----------

type SimpleAppSettings struct {
	Type     string `json:"type,omitempty"`
	ClientID string `json:"client_id,omitempty"`
}

type AppSettings struct {
	App *SimpleAppSettings `json:"app,omitempty"`
	// oauth e preservado como bruto para nao destruir configuracao OIDC que o
	// provider nao modela, ja que o PUT exige o corpo inteiro.
	OAuth json.RawMessage `json:"oauth,omitempty"`
}

type Application struct {
	ID          string       `json:"id,omitempty"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Type        string       `json:"type,omitempty"`
	Groups      []string     `json:"groups,omitempty"`
	Settings    *AppSettings `json:"settings,omitempty"`
	Status      string       `json:"status,omitempty"`
	CreatedAt   int64        `json:"created_at,omitempty"`
	UpdatedAt   int64        `json:"updated_at,omitempty"`
}

func (c *Client) CreateApplication(ctx context.Context, in Application) (*Application, error) {
	var out Application
	if err := c.do(ctx, http.MethodPost, "/applications", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetApplication(ctx context.Context, id string) (*Application, error) {
	var out Application
	if err := c.do(ctx, http.MethodGet, "/applications/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateApplication manda o corpo inteiro de proposito: UpdateApplicationEntity
// exige name, description e settings, e um PUT parcial zera settings.
func (c *Client) UpdateApplication(ctx context.Context, id string, in Application) (*Application, error) {
	var out Application
	if err := c.do(ctx, http.MethodPut, "/applications/"+url.PathEscape(id), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteApplication(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/applications/"+url.PathEscape(id), nil, nil)
}

// ---------- referencia polimorfica ----------

// Ref e uma referencia a outro objeto (api, plano, application).
//
// A Management API 3.x devolve o MESMO schema Subscription em tres formas, e o
// swagger declara as tres como string:
//
//	GET  .../subscriptions        -> api/plan/application como string (uuid)
//	GET  .../subscriptions/{id}   -> api/plan como objeto, application como null
//	POST .../subscriptions        -> api como objeto
//
// Ref aceita as duas formas e sempre serializa como o uuid nu.
type Ref struct {
	ID string
}

func (r *Ref) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		r.ID = ""
		return nil
	}
	if b[0] == '"' {
		return json.Unmarshal(b, &r.ID)
	}
	var o struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &o); err != nil {
		return fmt.Errorf("referencia nao e string nem objeto com id: %w", err)
	}
	r.ID = o.ID
	return nil
}

func (r Ref) MarshalJSON() ([]byte, error) { return json.Marshal(r.ID) }

func (r Ref) String() string { return r.ID }

// ---------- subscription ----------

type Subscription struct {
	ID          string `json:"id,omitempty"`
	API         Ref    `json:"api,omitempty"`
	Plan        Ref    `json:"plan,omitempty"`
	Application Ref    `json:"application,omitempty"`
	Status      string `json:"status,omitempty"`
	ClientID    string `json:"client_id,omitempty"`
	CreatedAt   int64  `json:"created_at,omitempty"`
	ClosedAt    int64  `json:"closed_at,omitempty"`
}

// SubscriptionGone diz se a subscription deve ser tratada como ausente.
//
// DELETE em subscription e "Close the subscription": o registro sobrevive com
// status CLOSED. Sem isso, um destroy seguido de apply veria recurso fantasma e
// nunca convergiria.
func SubscriptionGone(s *Subscription) bool {
	return s == nil || s.Status == "CLOSED" || s.Status == "REJECTED"
}

func (c *Client) CreateSubscription(ctx context.Context, appID, planID string) (*Subscription, error) {
	var out Subscription
	p := fmt.Sprintf("/applications/%s/subscriptions?plan=%s",
		url.PathEscape(appID), url.QueryEscape(planID))
	if err := c.do(ctx, http.MethodPost, p, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSubscription(ctx context.Context, appID, subID string) (*Subscription, error) {
	var out Subscription
	p := fmt.Sprintf("/applications/%s/subscriptions/%s", url.PathEscape(appID), url.PathEscape(subID))
	if err := c.do(ctx, http.MethodGet, p, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CloseSubscription e o DELETE da API, que fecha em vez de remover.
func (c *Client) CloseSubscription(ctx context.Context, appID, subID string) error {
	p := fmt.Sprintf("/applications/%s/subscriptions/%s", url.PathEscape(appID), url.PathEscape(subID))
	return c.do(ctx, http.MethodDelete, p, nil, nil)
}

// FindSubscription procura por par application/plano. Serve ao import, que
// recebe "<appID>:<planID>", e a deteccao de subscription preexistente.
func (c *Client) FindSubscription(ctx context.Context, appID, planID string) (*Subscription, error) {
	var out struct {
		Data []Subscription `json:"data"`
	}
	p := "/applications/" + url.PathEscape(appID) + "/subscriptions"
	raw := json.RawMessage{}
	if err := c.do(ctx, http.MethodGet, p, nil, &raw); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		// a 3.15 devolve lista nua em alguns casos
		var lista []Subscription
		if err2 := json.Unmarshal(raw, &lista); err2 != nil {
			return nil, fmt.Errorf("decodificando subscriptions de %s: %w", appID, err)
		}
		out.Data = lista
	}
	for i := range out.Data {
		s := out.Data[i]
		if s.Plan.ID == planID && !SubscriptionGone(&s) {
			return &s, nil
		}
	}
	return nil, nil
}

// ---------- api ----------

// Campos do export que o servidor possui. Nao entram na comparacao de drift e
// sao removidos antes de qualquer write.
var camposDoServidor = []string{"id", "primaryOwner", "members", "pages"}

// camposDoServidorNoPlano sao os campos de plano que o APIM atribui. O `id` e
// um caso especial: ele nao faz parte do desejado, mas PRECISA ser reenviado no
// update, senao o import trata o plano como novo e apaga o antigo junto com as
// subscriptions. Ver PareiaPlanos.
//
// `order` esta aqui porque o import NAO o honra: enviando order 0 e 1, os dois
// planos voltam com order 0. Mantê-lo na comparacao faria toda API com plano
// ordenado falhar o apply. Ver OrdemDeclaradaIgnorada, que avisa quando o
// declarado nao sobreviveu.
var camposDoServidorNoPlano = []string{"created_at", "updated_at", "api", "order"}

// OrdemDeclaradaIgnorada lista os planos cujo `order` declarado nao foi aplicado.
// O import zera a ordem, e quem declarou precisa saber que nao pegou.
func OrdemDeclaradaIgnorada(desejado, aplicado APIRaw) []string {
	aplicadoPorNome := map[string]float64{}
	if planos, ok := aplicado["plans"].([]any); ok {
		for _, p := range planos {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			nome, _ := m["name"].(string)
			if o, ok := m["order"].(float64); ok && nome != "" {
				aplicadoPorNome[nome] = o
			}
		}
	}
	var fora []string
	if planos, ok := desejado["plans"].([]any); ok {
		for _, p := range planos {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			nome, _ := m["name"].(string)
			quis, temOrdem := m["order"].(float64)
			if !temOrdem || nome == "" {
				continue
			}
			if tem, existe := aplicadoPorNome[nome]; existe && tem != quis {
				fora = append(fora, fmt.Sprintf("%s: declarado %.0f, aplicado %.0f", nome, quis, tem))
			}
		}
	}
	return fora
}

// AlinhaPlanos reordena os planos de `aplicado` para seguir a ordem de nomes de
// `desejado`, e devolve os nomes declarados que nao existem no aplicado.
//
// E necessario porque o export devolve os planos em ordem propria, nao na ordem
// enviada: comparar por posicao daria divergencia em toda API com mais de um
// plano. Plano e conjunto com chave `name` -- e o mesmo pareamento que
// PareiaPlanos usa para carregar os ids.
func AlinhaPlanos(desejado, aplicado APIRaw) (APIRaw, []string) {
	planosAplicados, ok := aplicado["plans"].([]any)
	if !ok {
		return aplicado, nil
	}
	porNome := map[string]any{}
	for _, p := range planosAplicados {
		if m, ok := p.(map[string]any); ok {
			if nome, _ := m["name"].(string); nome != "" {
				porNome[nome] = p
			}
		}
	}
	out := APIRaw{}
	for k, v := range aplicado {
		out[k] = v
	}
	var alinhados []any
	var faltando []string
	if planosDesejados, ok := desejado["plans"].([]any); ok {
		for _, p := range planosDesejados {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			nome, _ := m["name"].(string)
			if ap, achou := porNome[nome]; achou {
				alinhados = append(alinhados, ap)
				delete(porNome, nome)
			} else {
				faltando = append(faltando, nome)
			}
		}
	}
	// os que sobraram no servidor vao ao fim, para a comparacao de tamanho
	// ainda acusar plano a mais
	for _, p := range planosAplicados {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		nome, _ := m["name"].(string)
		if _, aindaSobra := porNome[nome]; aindaSobra {
			alinhados = append(alinhados, p)
			delete(porNome, nome)
		}
	}
	out["plans"] = alinhados
	return out, faltando
}

// APIRaw e a definicao de uma API como o export/import a serializa. O provider
// nao a interpreta: policy nenhuma e modelada, porque a `configuration` de cada
// policy e um objeto arbitrario.
type APIRaw map[string]any

func (c *Client) ExportAPI(ctx context.Context, id string) (APIRaw, error) {
	var out APIRaw
	p := "/apis/" + url.PathEscape(id) + "/export"
	if err := c.do(ctx, http.MethodGet, p, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateAPI usa POST /apis/import, que cria a API a partir de uma definicao.
func (c *Client) CreateAPI(ctx context.Context, def APIRaw) (APIRaw, error) {
	var out APIRaw
	if err := c.do(ctx, http.MethodPost, "/apis/import", def, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateAPI usa PUT /apis/{id}/import de proposito.
//
// PUT /apis/{id} responde 200 e apaga os flows dos planos, deixando a API
// inutilizavel. O unico write seguro numa API e o import.
func (c *Client) UpdateAPI(ctx context.Context, id string, def APIRaw) (APIRaw, error) {
	var out APIRaw
	p := "/apis/" + url.PathEscape(id) + "/import"
	if err := c.do(ctx, http.MethodPut, p, def, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DeleteAPI(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/apis/"+url.PathEscape(id), nil, nil)
}

func (c *Client) StopAPI(ctx context.Context, id string) error {
	p := "/apis/" + url.PathEscape(id) + "?action=STOP"
	return c.do(ctx, http.MethodPost, p, nil, nil)
}

// DeployAPI empurra a definicao para os gateways.
//
// Precisa ser chamado explicitamente: alterar plano nao mexe no `updated_at` da
// API, entao o gateway nunca recarrega sozinho.
func (c *Client) DeployAPI(ctx context.Context, id string) error {
	p := "/apis/" + url.PathEscape(id) + "/deploy"
	return c.do(ctx, http.MethodPost, p, map[string]any{}, nil)
}

type APIState struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContextPath string `json:"context_path"`
	State       string `json:"state"`
	UpdatedAt   int64  `json:"updated_at"`
	// IsSynchronized indica se o que esta no gateway bate com a definicao.
	// Ausente em algumas respostas, por isso ponteiro.
	IsSynchronized *bool `json:"is_synchronized,omitempty"`
}

func (c *Client) GetAPIState(ctx context.Context, id string) (*APIState, error) {
	var out APIState
	if err := c.do(ctx, http.MethodGet, "/apis/"+url.PathEscape(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Sincronizada diz se o gateway ja tem a definicao corrente.
func (c *Client) Sincronizada(ctx context.Context, id string) (bool, error) {
	var out struct {
		IsSynchronized bool `json:"is_synchronized"`
	}
	if err := c.do(ctx, http.MethodGet, "/apis/"+url.PathEscape(id)+"/state", nil, &out); err != nil {
		return false, err
	}
	return out.IsSynchronized, nil
}

// LimpaCamposDoServidor devolve uma copia sem os campos que o APIM atribui,
// para que a comparacao de drift veja so o que foi declarado.
func LimpaCamposDoServidor(def APIRaw) APIRaw {
	out := APIRaw{}
	for k, v := range def {
		out[k] = v
	}
	for _, k := range camposDoServidor {
		delete(out, k)
	}
	if planos, ok := out["plans"].([]any); ok {
		novos := make([]any, 0, len(planos))
		for _, p := range planos {
			m, ok := p.(map[string]any)
			if !ok {
				novos = append(novos, p)
				continue
			}
			c := map[string]any{}
			for k, v := range m {
				c[k] = v
			}
			for _, k := range camposDoServidorNoPlano {
				delete(c, k)
			}
			delete(c, "id")
			novos = append(novos, c)
		}
		out["plans"] = novos
	}
	return out
}

// PlanoDescartado e um plano que existe hoje e que o import apagaria.
type PlanoDescartado struct {
	ID       string
	Nome     string
	Security string
}

// PareiaPlanos injeta no desejado o `id` dos planos que ja existem, pareando
// por nome, e devolve os planos que o import apagaria.
//
// Isto nao e conveniencia: o import pareia planos por `id` e remove todo plano
// ausente do payload. Sem reenviar o id, um update recria os planos e as
// subscriptions vao com eles.
func PareiaPlanos(desejado, atual APIRaw) (APIRaw, []PlanoDescartado, error) {
	atuaisPorNome := map[string]map[string]any{}
	if planos, ok := atual["plans"].([]any); ok {
		for _, p := range planos {
			m, ok := p.(map[string]any)
			if !ok {
				continue
			}
			nome, _ := m["name"].(string)
			if nome == "" {
				continue
			}
			if _, dup := atuaisPorNome[nome]; dup {
				return nil, nil, fmt.Errorf("a API tem mais de um plano chamado %q; o pareamento por nome fica ambiguo e o import apagaria um deles", nome)
			}
			atuaisPorNome[nome] = m
		}
	}

	out := APIRaw{}
	for k, v := range desejado {
		out[k] = v
	}
	usados := map[string]bool{}
	if planos, ok := out["plans"].([]any); ok {
		novos := make([]any, 0, len(planos))
		for _, p := range planos {
			m, ok := p.(map[string]any)
			if !ok {
				novos = append(novos, p)
				continue
			}
			c := map[string]any{}
			for k, v := range m {
				c[k] = v
			}
			nome, _ := c["name"].(string)
			if at, achou := atuaisPorNome[nome]; achou {
				if id, _ := at["id"].(string); id != "" {
					c["id"] = id
					usados[nome] = true
				}
			}
			novos = append(novos, c)
		}
		out["plans"] = novos
	}

	var descartados []PlanoDescartado
	for nome, m := range atuaisPorNome {
		if usados[nome] {
			continue
		}
		id, _ := m["id"].(string)
		sec, _ := m["security"].(string)
		descartados = append(descartados, PlanoDescartado{ID: id, Nome: nome, Security: sec})
	}
	sortDescartados(descartados)
	return out, descartados, nil
}

func sortDescartados(d []PlanoDescartado) {
	for i := 1; i < len(d); i++ {
		for j := i; j > 0 && d[j].Nome < d[j-1].Nome; j-- {
			d[j], d[j-1] = d[j-1], d[j]
		}
	}
}

// ---------- plano ----------

type Plano struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Security string `json:"security"`
	Status   string `json:"status"`
}

func (c *Client) ListPlans(ctx context.Context, apiID string) ([]Plano, error) {
	var out []Plano
	p := "/apis/" + url.PathEscape(apiID) + "/plans?status=published,staging,deprecated"
	if err := c.do(ctx, http.MethodGet, p, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ClosePlan fecha um plano. Fechar plano fecha as subscriptions dele.
//
// E obrigatorio antes de apagar a API: o DELETE responde 400 com
// "Plan(s) [...] must be closed before being able to delete the API".
func (c *Client) ClosePlan(ctx context.Context, apiID, planID string) error {
	p := fmt.Sprintf("/apis/%s/plans/%s/_close", url.PathEscape(apiID), url.PathEscape(planID))
	return c.do(ctx, http.MethodPost, p, nil, nil)
}
