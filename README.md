# Guardei

Bot de Telegram em Go para salvar links de vídeos (Instagram, TikTok, YouTube, X) e achá-los depois por busca em linguagem natural. Transcrição opcional com Gemini. Self-hosted, leve, SQLite.

Conteúdo salvo em vários apps se perde, porque cada um tem sua lista e uma busca fraca. Aqui há um lugar só, o seu chat com o bot: cada link entra com uma descrição do que o vídeo trata, e você acha depois escrevendo o que lembra ("aquele vídeo do pão de queijo").

- **Leve:** um binário Go e um arquivo SQLite. Sem Postgres, sem CGO.
- **IA opcional:** sem `GEMINI_API_KEY` funciona só com descrição manual e busca full-text.
- **Degradar, não falhar:** qualquer problema de extração cai no pedido de descrição.
- **Uso pessoal:** só os usuários de `ALLOWED_USER_IDS` são atendidos.

## Como funciona

| Você envia | O bot faz |
| --- | --- |
| Link do YouTube, TikTok ou X, sozinho | Baixa o áudio, transcreve com o Gemini e salva título, resumo e tags. |
| Link + texto na mesma mensagem | Salva com o seu texto como descrição, sem perguntar nada. |
| Link do Instagram ou de qualquer outro site | Pede uma descrição e salva. |
| Texto sem link | Busca nos itens salvos. |

Se a extração não for possível (sem chave, vídeo longo, sem fala, plataforma bloqueando), o bot diz o motivo e pede a descrição. Um link repetido (mesma URL canônica) avisa que já está salvo e oferece atualizar a descrição.

A busca é sempre full-text (SQLite FTS5, sem diferenciar acentos, com prefixo). Com chave Gemini ela vira híbrida: soma a busca semântica (embeddings) por *reciprocal rank fusion*, e assim "comida mineira" acha um vídeo de pão de queijo.

## Começando

### 1. Crie o bot e descubra seu ID

1. No Telegram, abra o **@BotFather**, envie `/newbot` e siga os passos. Ele devolve o **token**.
2. Abra o **@userinfobot** e anote o seu **Id** numérico.
3. Abra o seu bot novo e toque em **Iniciar**.

### 2. Configure

```bash
cp .env.example .env
# preencha TELEGRAM_BOT_TOKEN e ALLOWED_USER_IDS (e GEMINI_API_KEY, se quiser IA)
```

A chave do Gemini vem do [Google AI Studio](https://aistudio.google.com/apikey).

### 3. Rode

**Com Docker** (inclui `yt-dlp` e `ffmpeg`):

```bash
docker compose up -d --build
docker compose logs -f
```

Os dados ficam no volume `guardei-data`. Também funciona com Podman.

**Sem Docker**, com Go 1.27+, `yt-dlp` e `ffmpeg` no `PATH` (sem eles o bot roda, mas só pede descrição):

```bash
set -a; . ./.env; set +a
go run ./cmd/bot
```

## Configuração

Tudo por variáveis de ambiente. Só as duas primeiras são obrigatórias.

| Variável | Padrão | Função |
| --- | --- | --- |
| `TELEGRAM_BOT_TOKEN` | — | Token do bot (BotFather). |
| `ALLOWED_USER_IDS` | — | IDs do Telegram autorizados, separados por vírgula. |
| `GEMINI_API_KEY` | vazio | Vazio liga o modo manual, sem IA e sem enviar nada ao Google. |
| `GEMINI_MODEL` | `gemini-2.5-flash-lite` | Modelo de transcrição, resumo e tags. |
| `GEMINI_EMBEDDING_MODEL` | `gemini-embedding-2` | Modelo de embeddings (768 dimensões). |
| `MAX_VIDEO_SECONDS` | `600` | Vídeos mais longos caem no pedido de descrição. |
| `YTDLP_PATH` | `yt-dlp` | Caminho do binário. |
| `SEARCH_LIMIT` | `5` | Máximo de resultados por busca. |
| `DB_PATH` | `./data/app.db` | Arquivo SQLite. |

### Escolhendo os modelos

Os IDs de modelo do Gemini mudam com frequência, por isso ficam em variáveis. Liste o que a sua chave enxerga:

```bash
curl -s "https://generativelanguage.googleapis.com/v1beta/models?pageSize=200" \
  -H "x-goog-api-key: $GEMINI_API_KEY" | grep '"name"'
```

Os padrões foram escolhidos em setembro de 2026 pelo menor custo estável: o `gemini-2.5-flash-lite` cobra US$ 0,30 por milhão de tokens de áudio de entrada e US$ 0,40 por milhão de saída, e o `gemini-embedding-2` cobra US$ 0,20 por milhão de tokens de texto. Confira os preços atuais na [tabela oficial](https://ai.google.dev/gemini-api/docs/pricing) antes de confiar nesses números. Ao trocar de modelo, teste com alguns vídeos seus: a qualidade em português varia.

**Custo medido:** o áudio conta 32 tokens por segundo, então cada minuto de vídeo custa cerca de US$ 0,0006 de entrada, mais a saída. Um vídeo de 25 minutos custou por volta de US$ 0,02 na estimativa.

## Comandos

| Comando | Ação |
| --- | --- |
| Mensagem com link | Salva o item. |
| Mensagem sem link | Busca. |
| `/recentes` | Últimos 10 itens. |
| `/editar <id> [texto]` | Troca a descrição e reindexa. Sem texto, pede na próxima mensagem. |
| `/apagar <id>` | Remove o item. |
| `/cancelar` | Cancela a espera por descrição. |
| `/reindexar` | Gera resumo e embeddings dos itens salvos antes de a chave existir. |
| `/status` | Mostra se a IA está ligada e quantos itens existem. |

## Limitações

- **Instagram nunca é extraído:** o bot sempre pede descrição. É uma decisão do projeto. Habilitar no futuro é implementar outro `Extractor` e mudar uma flag em `internal/platform`.
- **TikTok e X são "melhor esforço":** o `yt-dlp` pode falhar sem aviso (IP bloqueado, post protegido, login exigido) e o bot cai na descrição. O X exige login para quase tudo hoje, então espere falhas. Links curtos do TikTok (`vm.tiktok.com`) não são resolvidos, então a detecção de duplicatas não os reconhece.
- **Vídeos longos:** acima de `MAX_VIDEO_SECONDS` o bot pede descrição. O áudio é fatiado em pedaços de 5 minutos e cada um é transcrito à parte. Enviar 25 minutos de uma vez fez o modelo parar na metade ou entrar em repetição. Acima de uns 60 minutos o áudio passa de 15 MB e é recusado.
- **Áudio sem fala** (música, ruído) é descartado, e o bot pede descrição.
- O bot guarda só link, texto e metadados. Não guarda o vídeo.
- Sem stemmer para português no FTS5: a busca por prefixo e a semântica compensam. Sem chave, "corrida" não acha "correr".

## Privacidade e termos de uso

- **O áudio dos vídeos é enviado ao Google (Gemini).** Com `GEMINI_API_KEY` vazio, nada sai da sua máquina além das mensagens do Telegram.
- Extrair conteúdo de uma plataforma pode violar os termos de uso dela. O projeto é para uso pessoal e a extração é opt-in: sem chave ou sem `yt-dlp`, ela não acontece. A responsabilidade do uso é sua.

## Operação

**Backup.** O estado está em um arquivo SQLite. Use o `.backup` (seguro com o bot rodando) e copie para fora da VPS:

```bash
docker compose exec guardei sqlite3 /data/app.db ".backup /data/backup.db"
docker compose cp guardei:/data/backup.db "./backup-$(date +%F).db"
```

**yt-dlp.** Ele quebra sempre que uma plataforma muda. O bot roda `yt-dlp -U` toda semana enquanto estiver no ar. Numa atualização da imagem, use `docker compose build --pull` e, se precisar de uma versão específica, `--build-arg YTDLP_VERSION=<versão>`.

**Recursos** (medidos em container com limite de 512 MB):

| | Medido |
| --- | --- |
| Imagem, com `yt-dlp` e `ffmpeg` | ~199 MB |
| RAM do bot ocioso ou buscando | 8 a 15 MB |
| Pico durante uma transcrição | ~100 MB de processos (bot + `yt-dlp` ~81 MB, ou `ffmpeg` ~69 MB) |
| Pico do container, contando cache de arquivos | 183 MiB, sem estourar o limite |

Só há uma extração de áudio por vez, e `yt-dlp` e `ffmpeg` rodam em sequência, por isso o pico não soma os dois. O uso normal é de poucos MB, mas uma extração passa dos 100 MB por alguns segundos. Uma VPS de 512 MB dá conta, e uma de 256 MB provavelmente não.

## Desenvolvimento

```bash
go vet ./... && go test ./...
```

Os testes com a API real ficam ignorados sem as variáveis. Para rodá-los:

```bash
set -a; . ./.env; set +a
go test ./internal/ai -run Live -v                       # Gemini: texto e áudio
LIVE_VIDEO_URLS="https://youtu.be/..." LIVE_QUERIES="termo;outro termo" \
  go test ./internal/bot -run Live -v                    # ponta a ponta, com yt-dlp
```

Estrutura:

```
cmd/bot/            ponto de entrada
internal/bot/       handlers do Telegram e máquina de estados
internal/platform/  detecção de plataforma e URL canônica
internal/extract/   yt-dlp + ffmpeg (download, conversão, fatiamento)
internal/ai/        AIClient, Gemini e a versão sem IA
internal/store/     SQLite, migrações, FTS5
internal/search/    FTS, cosseno e RRF
migrations/         SQL aplicado na inicialização
```

## Licença

[MIT](LICENSE)
