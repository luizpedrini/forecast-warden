# forecast-warden — Spec mínima (fatia 1)

**Status:** fatia 1 shipped (MIT)  
**Escopo:** OSS genérico de logística / demand forecasting ops — zero dados ou IP de qualquer empresa  
**Objetivo da fatia:** num sábado, ter um CLI que lê métricas sintéticas de um run de forecast, detecta anomalias simples e emite um **Forecast Incident** (spec) aprovável por humano.

---

## 1. Problema

Times de forecast gastam energia em *modelo* e pouco em *gestão do ciclo*: drift, bias, feature quebrada, calendário. O conhecimento fica no Slack/cabeça. Falta um artefato padrão (incident) + gate humano antes de promover/retreinar.

## 2. Não-objetivos (fatia 1)

- Treinar ou servir modelos
- Feature store / Feast
- LLM obrigatório (narrativa pode ser template)
- Integração com frota/promise (fase 2+)
- UI web
- Dados reais de qualquer empresa

## 3. Personas

- **Forecast / DS owner** — roda o warden após o batch diário
- **EM / tech lead** — lê incidents na ritual semanal; aprova ações
- **Contribuidor OSS** — pluga novo detector via contrato claro

## 4. Conceitos

| Conceito | Definição |
|----------|-----------|
| **Run** | Um ciclo de forecast (ex. D+1) com métricas agregadas |
| **Entity** | Chave de análise: `zone` (MVP); depois `sku`, `node` |
| **Signal** | Métrica no run: mape, bias, coverage, n_actuals, … |
| **Detector** | Regra pura: signals → lista de findings |
| **Finding** | Anomalia tipada com evidência numérica |
| **Incident** | Documento versionado que agrupa findings + hipótese + ação sugerida + status |

## 5. Ritual (mais importante que o código)

1. Após o batch: `warden check --run <path>`  
2. Se houver finding ≥ `warning`: gera incident `open`  
3. Ritual semanal (15 min): humanos triam incidents `open`  
4. **Gate:** promover modelo / mudar config só com incident crítico `resolved` ou `accepted-risk`  
5. DoR do merge de “fix do warden”: teste de leakage/PIT não se aplica ainda; vale teste do detector + golden incident

## 6. Contratos de dados (sintéticos)

### 6.1 `run_metrics.parquet` ou `.csv`

| coluna | tipo | notas |
|--------|------|-------|
| run_id | string | ex. `2026-09-12` |
| entity_type | string | `zone` |
| entity_id | string | `Z1`… |
| mape | float | 0–1 ou % — documentar unidade |
| bias | float | mean(pred-actual)/mean(actual) |
| coverage_80 | float | % de actuals no intervalo 80% |
| n_actuals | int | suporte amostral |
| generated_at | iso8601 | event time do run |

### 6.2 `baseline_stats.csv` (opcional fatia 1)

Rolling mean/std de mape/bias por entity (janela 28d sintética) para z-score.

## 7. Detectors (MVP — rule-based)

1. **HighMAPE** — `mape > threshold` (default 0.25) e `n_actuals >= 30`  
2. **BiasShift** — `|bias| > 0.15` ou z-score vs baseline > 3  
3. **LowSupport** — `n_actuals < 30` → finding `info` (não abre incident sozinho)  
4. **CoverageBreak** — `coverage_80 < 0.60` com suporte ok  

Cada finding: `{code, severity, entity_id, evidence{}, hint}`

Severities: `info` | `warning` | `critical`

## 8. Forecast Incident (artefato)

Arquivo: `incidents/<run_id>__<short-hash>.md` (+ `.json` espelho machine-readable)

Frontmatter JSON/YAML:

```yaml
id: fw-20260912-a1b2
run_id: "2026-09-12"
status: open  # open | resolved | accepted-risk | wontfix
severity: warning
entities: ["Z3", "Z7"]
detector_codes: ["HighMAPE", "BiasShift"]
created_at: ...
```

Corpo (template):

- Sintoma (1 parágrafo)
- Evidência (tabela dos findings)
- Hipóteses candidatas (lista fechada na fatia 1: `demand_spike`, `feature_break`, `calendar`, `model_stale`, `data_delay`)
- Ação sugerida (enum: `investigate`, `hold_promotion`, `retune_threshold`, `retrain`, `accepted_risk`)
- Gate: o que fica bloqueado enquanto `open`

## 9. CLI

```
warden init          # fixtures sintéticos + config default
warden check         # lê metrics, roda detectors, escreve incidents se preciso
warden list          # incidents por status
warden show <id>
warden resolve <id> --note "..."
```

Exit code: `0` ok, `1` warnings, `2` critical (útil em CI).

## 10. DoR / DoD da fatia 1

**Ready**
- [x] Spec aprovada (este doc)
- [x] Nome do repo decidido (`forecast-warden`)
- [x] Licença MIT

**Done**
- [x] `warden init` gera 14 dias de métricas sintéticas (algumas zonas "doentes")
- [x] `warden check` produz ≥1 incident golden em fixture
- [x] Testes unitários dos 4 detectors
- [x] README: problema, ritual, quickstart, non-goals
- [x] Sem LLM, sem rede obrigatória

## 11. Fora / depois

- LLM narra o incident  
- Ponte forecast-to-fleet  
- Plug Feast/metrics store  
- Hierarquia SKU×nó  
- UI

## 12. Decisão pedida ao Luiz

1. Confirma fatia 1 como acima?  
2. Threshold defaults ok ou prefere só config sem defaults “mágicos”?  
3. Licença: MIT vs Apache-2.0?  
4. Começar a codar a fatia 1 no próximo bloco?

---

## 13. DoR de sessão + ritual de gate (Spec Coach)

### DoR (go/no-go de kickoff — 10 min)
1. Problema em 1 frase + não-metas explícitas (só sintético, rule-based, sem IP corporativo).
2. Outcome observável da fatia + fora de escopo escrito.
3. Aceitação em linguagem de demo (5 min), não de API.
4. Humano vs agente: o que o warden fecha sozinho vs o que exige gate.
5. Critério de abort / re-slice (ex.: detectors + template sem CLI ainda).

### Ritual
- **Kickoff:** ler DoR; dono confirma shipável na sessão; go/no-go único.
- **Mid (opcional):** só se escopo inchou — revalidar fora de escopo.
- **Gate de pronto (demo 10–15 min):** incident gerado? CLI usável? schema estável? → done / done+follow-up / re-slice.
- **Retro 1 pergunta:** o que faltou no DoR? → uma linha no playbook.

### Papéis fatia 1
- **Agente/CLI:** detecta, emite incident `open`, lista/mostra.
- **Humano:** `resolve` / `accepted-risk`; decide promoção de modelo (fora do repo ainda).
