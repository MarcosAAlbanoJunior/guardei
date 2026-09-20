# Guardei

[![CI](https://github.com/MarcosAAlbanoJunior/guardei/actions/workflows/ci.yml/badge.svg)](https://github.com/MarcosAAlbanoJunior/guardei/actions/workflows/ci.yml)

Bot de Telegram em Go para salvar vídeos e posts (Instagram, TikTok, YouTube, X, LinkedIn e qualquer página) e achá-los depois por busca em linguagem natural. Transcrição e análise opcionais com Gemini. Self-hosted, leve, SQLite.

## Passo a passo: do zero ao bot rodando

Você vai precisar de uma conta no Telegram, de uma VPS Linux e, se quiser IA, de uma conta Google. Não é preciso saber programar: são só comandos para copiar e colar.

### 1. Crie o bot no Telegram e pegue o token

1. No Telegram (celular ou computador), procure por **@BotFather**, o bot oficial com selo azul de verificado, e toque em **Iniciar**.
2. Envie `/newbot`.
3. Ele pede um **nome** de exibição. Pode ser `Guardei`.
4. Ele pede um **username**, que precisa terminar em `bot`, por exemplo `guardei_seunome_bot`. Se já existir, tente outro.
5. Ele responde com o **token**, algo como `123456789:AAH...`. Copie e guarde: é o valor de `TELEGRAM_BOT_TOKEN`.

> O token dá controle total sobre o bot. Não publique nem compartilhe. Se vazar, envie `/revoke` ao BotFather e gere outro.

### 2. Descubra o seu ID do Telegram

O bot só atende quem estiver na lista de autorizados, e a lista usa o ID numérico, não o nome.

1. Procure por **@userinfobot** e toque em **Iniciar**.
2. Ele responde com o seu **Id**, um número como `123456789`. Anote: é o valor de `ALLOWED_USER_IDS`.

Depois, abra o **seu bot novo** (o link `t.me/...` que o BotFather deu) e toque em **Iniciar**. Sem isso ele não recebe mensagens suas.

### 3. (Opcional, recomendado) Crie uma chave do Gemini

Sem a chave, o bot funciona no **modo manual**: você envia o link, escreve uma descrição, e ele guarda e busca por texto. Com a chave, ele **transcreve vídeos, lê posts, gera resumo e tags e entende buscas por sentido**.

1. Acesse o [Google AI Studio](https://aistudio.google.com/apikey) e entre com sua conta Google.
2. Clique em **Create API key** (Criar chave de API). O nome dos botões pode variar um pouco.
3. Copie a chave, que começa com `AIza`. É o valor de `GEMINI_API_KEY`.

Sobre custo e privacidade:

- O uso é cobrado por volume, e barato: cerca de **US$ 0,0006 por minuto de vídeo** de entrada, mais uma saída pequena (estimativa com o modelo padrão; confira a [tabela oficial](https://ai.google.dev/gemini-api/docs/pricing)).
- **No plano gratuito, o Google pode usar o que você envia para melhorar seus produtos, e revisores humanos podem ler.** No plano pago, não. O bot envia o áudio dos vídeos e o texto dos posts, então, se o conteúdo for sensível, ative o faturamento no projeto da chave. Veja os [termos](https://ai.google.dev/gemini-api/terms).

### 4. Suba o bot em uma VPS

**Escolha a VPS.** Qualquer provedor serve. Peça uma máquina com **Ubuntu 24.04 (ou 22.04) ou Debian 12** e **1 GB de RAM**. O bot em si usa poucos MB, mas **a construção da imagem precisa de cerca de 1 GB** (veja "Problemas comuns" se sua VPS tem só 512 MB). O bot só faz conexões de saída, então **não precisa abrir nenhuma porta**.

**4.1. Entre na VPS.** O provedor informa o IP e a senha (ou chave SSH). No terminal do seu computador (no Windows, use o PowerShell):

```bash
ssh root@IP_DA_VPS
```

**4.2. Instale o Docker** (script oficial do Docker; instala também o `docker compose`):

```bash
curl -fsSL https://get.docker.com | sh
```

**4.3. Baixe o projeto:**

```bash
git clone https://github.com/MarcosAAlbanoJunior/guardei.git
cd guardei
```

Se aparecer `git: command not found`, rode `apt-get update && apt-get install -y git` e repita.

**4.4. Configure.** Copie o modelo e abra para editar:

```bash
cp .env.example .env
nano .env
```

Preencha as três linhas com o que você guardou (sem aspas e sem espaços):

```
TELEGRAM_BOT_TOKEN=123456789:AAH...
ALLOWED_USER_IDS=123456789
GEMINI_API_KEY=AIza...
```

Para salvar no `nano`: `Ctrl+O`, `Enter`, `Ctrl+X`. Sem chave do Gemini, deixe `GEMINI_API_KEY=` vazio.

Opcional: acrescente `TZ=America/Sao_Paulo` (ou o seu fuso) para que "hoje" e "ontem" nas buscas sigam o seu horário. Sem isso, valem as datas em UTC. Depois proteja o arquivo:

```bash
chmod 600 .env
```

**4.5. Ligue:**

```bash
docker compose up -d --build
```

Na primeira vez, ele baixa as dependências e constrói a imagem. Leva alguns minutos.

**4.6. Confirme que subiu:**

```bash
docker compose logs --tail 20
```

Você deve ver uma linha assim:

```
INFO bot iniciado db=/data/app.db ia=true vetores=0 extracao=true
```

`ia=true` quer dizer que a chave do Gemini foi aceita (`ia=false` = modo manual). `extracao=true` quer dizer que o download de áudio está pronto. O bot fica rodando sozinho e volta sozinho se a VPS reiniciar.

### 5. Teste

No Telegram, abra o seu bot e:

1. Envie `/status`. Ele deve responder com `IA: ligada` (ou `desligada`, se você não pôs chave) e `Itens salvos: 0`.
2. Envie o link de um vídeo do YouTube em português, de até 10 minutos. Ele responde `⏳ Baixando o áudio e transcrevendo…` e depois `Salvo (#1, youtube, transcrito)`.
3. Envie o link de um post do LinkedIn ou do X. Ele lê o texto e salva.
4. Escreva algo que lembre do que salvou, como `dicas de css`, sem link. Ele devolve o item.
5. Salve uns 8 itens sobre o mesmo assunto e busque por ele: o bot mostra 5 e um botão **Ver mais**.

### Dia a dia na VPS

Rode os comandos dentro da pasta `guardei`:

| Quero | Comando |
| --- | --- |
| Ver o que o bot está fazendo | `docker compose logs -f` (saia com `Ctrl+C`; o bot continua) |
| Atualizar para a versão nova | `git pull && docker compose up -d --build` |
| Trocar token, ID ou chave | edite o `.env` e rode `docker compose up -d --force-recreate` |
| Parar | `docker compose stop` (para voltar: `docker compose start`) |
| Fazer backup | veja [Backup](#backup) |

> Cuidado: `docker compose down -v` **apaga o banco** com tudo que você salvou. Sem o `-v`, os dados ficam guardados.

### Problemas comuns

| Sintoma | Causa e solução |
| --- | --- |
| O bot não responde nada | Rode `docker compose logs --tail 50`. Se aparecer `usuário não autorizado user_id=NNN`, o seu ID não está em `ALLOWED_USER_IDS`: coloque esse número lá e recrie (`docker compose up -d --force-recreate`). Confira também se você tocou em **Iniciar** no bot. |
| O bot para logo ao ligar | Token errado ou vazio no `.env`. Os logs mostram `error call getMe, unauthorized` (token inválido) ou `TELEGRAM_BOT_TOKEN é obrigatória` (vazio). |
| Os logs mostram `ia=false` | A `GEMINI_API_KEY` está vazia ou com erro de digitação. Corrija o `.env` e recrie. |
| A construção falha com `signal: killed` | Faltou memória (VPS de 512 MB). Crie um swap temporário e rode o `up` de novo: `fallocate -l 1G /swapfile && chmod 600 /swapfile && mkswap /swapfile && swapon /swapfile`. No meu teste, com limite de 512 MB de RAM mais 1 GB de swap, a construção terminou em 45 s; sem swap, o compilador foi morto. |
| Os logs repetem `Conflict: terminated by other getUpdates request` | Há outra cópia do bot usando o mesmo token (por exemplo no seu computador). Só uma instância por token: pare a outra. |
| `docker: command not found` | Faltou o passo 4.2. |
| Erros `429` do Gemini | Cota do plano gratuito esgotada. Espere alguns minutos ou ative o faturamento. |
| Um link cai no pedido de descrição | É o comportamento planejado quando o post é privado, o site pede login ou o vídeo é longo. O bot diz o motivo; responda com uma descrição. Veja [Limitações](#limitações). |

---

## Como funciona

- **Leve:** um binário Go e um arquivo SQLite. Sem Postgres, sem CGO.
- **IA opcional:** sem `GEMINI_API_KEY` funciona só com descrição manual e busca full-text.
- **Degradar, não falhar:** qualquer problema de extração cai no pedido de descrição.
- **Uso pessoal:** só os usuários de `ALLOWED_USER_IDS` são atendidos.

Conteúdo salvo em vários apps se perde, porque cada um tem sua lista e uma busca fraca. Aqui há um lugar só, o seu chat com o bot: cada link entra com uma descrição do que ele trata, e você acha depois escrevendo o que lembra ("aquele vídeo do pão de queijo", "aquele post sobre CSS").

| Você envia | O bot faz |
| --- | --- |
| Link de vídeo do YouTube, TikTok ou X, sozinho | Baixa o áudio, transcreve com o Gemini e salva título, resumo e tags. Se o vídeo não tem fala, é longo demais, está ao vivo ou o áudio não pôde ser baixado, usa o **título e a descrição** do vídeo. |
| Link de post (Instagram, X, LinkedIn) ou de qualquer página, sozinho | Lê o texto público do post e o Gemini gera título, resumo e tags. |
| Link + texto na mesma mensagem | Salva com o seu texto como descrição, sem ler nada. |
| Texto sem link | Busca nos itens salvos. |

Para cada link o bot tenta o caminho automático: o áudio; se não servir, o título e a descrição do vídeo; e, para posts e páginas, o texto público. Se nada disso for possível (sem chave, post privado, site pedindo login ou bloqueando, ou quase nenhum texto), ele diz o motivo e pede a descrição, que você responde em texto. Um link repetido (mesma URL canônica) avisa que já está salvo e oferece atualizar a descrição.

A busca é sempre full-text (SQLite FTS5, sem diferenciar acentos, com prefixo, e `receitas` acha `receita`). Com chave Gemini ela vira híbrida: soma a busca semântica (embeddings) por *reciprocal rank fusion*, e assim "comida mineira" acha um vídeo de pão de queijo.

### Muitos resultados, períodos e plataformas

O bot mostra os resultados por página (5 por vez, `SEARCH_LIMIT`) e diz quantos existem:

```
Achei 50 itens · mostrando 1–5:

#12 · youtube · há 3 dias
Como fazer bolo de cenoura
https://youtube.com/...
```

Sob a mensagem aparecem botões: **Ver mais 5 ▶** traz a próxima página, e **Hoje**, **7 dias** e **30 dias** refazem a busca só naquele período (**Todo o período** volta ao normal). A lista fica guardada na memória por 30 minutos, então tocar nos botões é instantâneo e **não repete a busca nem chama a IA de novo**: só lê do banco os 5 itens da página. Depois de 30 minutos, ou de reiniciar o bot, o botão avisa que a busca expirou e basta repetir a busca.

Você também pode escrever o período ou a plataforma na própria busca:

| Escreva | O bot entende |
| --- | --- |
| `receitas da semana`, `receitas essa semana` | receitas dos últimos 7 dias |
| `receitas de hoje`, `receitas de ontem` | receitas de hoje ou de ontem |
| `semana passada`, `mês passado`, `deste mês` | o período correspondente |
| `últimos 15 dias`, `últimas 2 semanas` | os últimos N dias |
| `receitas do youtube`, `css no tiktok`, `só instagram`, `no x` | só aquela plataforma |
| `o que eu salvei hoje`, `só do youtube` | sem assunto: tudo do filtro, do mais novo ao mais antigo |
| `receitas do youtube da semana` | os dois filtros juntos |

O filtro aparece na resposta (`Filtro: YouTube · últimos 7 dias`), e **Todo o período** o remove. "Hoje" e "ontem" só viram filtro em construções como `de hoje`, `salvei hoje` ou no começo da busca; em `algo para cozinhar hoje` a palavra fica no assunto.

## Comandos

| Comando | Ação |
| --- | --- |
| Mensagem com link | Salva o item. |
| Mensagem sem link | Busca. |
| `/recentes [termo]` | Itens do mais novo ao mais antigo (10 por página), ou só os que casam com o termo. Aceita período e plataforma: `/recentes youtube hoje`. |
| `/editar <id> [texto]` | Troca a descrição e reindexa. Sem texto, pede na próxima mensagem. |
| `/apagar <id>` | Remove o item. |
| `/cancelar` | Cancela a espera por descrição. |
| `/reindexar` | Gera resumo e embeddings dos itens salvos antes de a chave existir. |
| `/status` | Mostra se a IA está ligada e quantos itens existem. |

## Configuração

Tudo por variáveis de ambiente, no arquivo `.env`. Só as duas primeiras são obrigatórias.

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
| `TZ` | UTC no Docker | Fuso horário de "hoje" e "ontem" nas buscas, por exemplo `America/Sao_Paulo`. |
| `DB_PATH` | `./data/app.db` | Arquivo SQLite (no Docker é `/data/app.db`, no volume). |

### Escolhendo os modelos

Os IDs de modelo do Gemini mudam com frequência, por isso ficam em variáveis. Liste o que a sua chave enxerga:

```bash
curl -s "https://generativelanguage.googleapis.com/v1beta/models?pageSize=200" \
  -H "x-goog-api-key: $GEMINI_API_KEY" | grep '"name"'
```

Os padrões foram escolhidos em setembro de 2026 pelo menor custo estável: o `gemini-2.5-flash-lite` cobra US$ 0,30 por milhão de tokens de áudio de entrada e US$ 0,40 por milhão de saída, e o `gemini-embedding-2` cobra US$ 0,20 por milhão de tokens de texto. Confira os preços atuais na [tabela oficial](https://ai.google.dev/gemini-api/docs/pricing) antes de confiar nesses números. Ao trocar de modelo, teste com alguns vídeos seus: a qualidade em português varia.

**Custo medido:** o áudio conta 32 tokens por segundo, então cada minuto de vídeo custa cerca de US$ 0,0006 de entrada, mais a saída. Um vídeo de 25 minutos custou por volta de US$ 0,02 na estimativa.

### Rodar sem Docker

Com Go 1.27+, `yt-dlp` e `ffmpeg` no `PATH` (sem eles o bot roda, mas só pede descrição):

```bash
cp .env.example .env   # e preencha
set -a; . ./.env; set +a
go run ./cmd/bot
```

## Limitações

- **Instagram:** o áudio de reels nunca é baixado. Para posts e reels, o bot só lê a legenda que o Instagram expõe publicamente nas tags de prévia do link, e só quando ela existe. Posts privados, sem legenda ou que o Instagram não entrega caem no pedido de descrição.
- **Posts e páginas:** o bot lê o texto público sem login. LinkedIn costuma entregar o post inteiro. O **X entrega só os primeiros ~300 caracteres** de posts longos, e o bot avisa (`/editar <id>` completa). Páginas que exigem login ou JavaScript para mostrar o conteúdo não funcionam. Sem `GEMINI_API_KEY` o bot não lê posts, só pede a descrição.
- **Só texto:** imagens, carrosséis e vídeos sem áudio em posts não são analisados, só a legenda.
- **Buscas:** cada busca lista no máximo 100 resultados (aparece `100+`). Buscas genéricas trazem muitos itens de relevância parecida, e a ordem entre eles é quase arbitrária: use os botões de período, os filtros escritos ou um termo mais específico. O corte de relevância da busca por sentido foi calibrado com poucos dados, e pode pedir ajuste na sua coleção.
- **TikTok e X são "melhor esforço":** o `yt-dlp` pode falhar sem aviso (IP bloqueado, post protegido, login exigido) e o bot cai na descrição. O X exige login para quase tudo hoje, então espere falhas. Links curtos do TikTok (`vm.tiktok.com`) não são resolvidos, então a detecção de duplicatas não os reconhece.
- **Vídeos longos:** acima de `MAX_VIDEO_SECONDS` o bot não transcreve e usa o título e a descrição do vídeo. O áudio é fatiado em pedaços de 5 minutos e cada um é transcrito à parte. Enviar 25 minutos de uma vez fez o modelo parar na metade ou entrar em repetição. Acima de uns 60 minutos o áudio passa de 15 MB e é recusado.
- **Vídeos sem fala** (música, ruído, ao vivo) e os que o YouTube não deixa baixar: o bot usa título, descrição completa, canal e tags do vídeo, lidos pelo `yt-dlp`. O item aparece como `por título e descrição`, e o resumo vem só desses textos (a IA é instruída a não afirmar o que o vídeo mostra ou diz), então vale conferir. Sem descrição nem título úteis, o bot pede a sua.
- O bot guarda só link, texto e metadados. Não guarda o vídeo.
- Sem stemmer para português no FTS5: a busca por prefixo e a semântica compensam. Sem chave, "corrida" não acha "correr".

## Privacidade e termos de uso

- **O áudio dos vídeos e o texto dos posts lidos são enviados ao Google (Gemini).** No plano gratuito da chave, o Google pode usar esse conteúdo para melhorar seus produtos e revisores humanos podem lê-lo; no plano pago, não (veja os [termos](https://ai.google.dev/gemini-api/terms)). Com `GEMINI_API_KEY` vazio, nada sai da sua máquina além das mensagens do Telegram, e o bot não baixa nem lê nada.
- Ao ler um post, o bot faz uma requisição HTTP ao site a partir do seu servidor, como uma prévia de link. Para o Instagram ele se identifica como o rastreador de prévia do Facebook (`facebookexternalhit`), o único que recebe a legenda; para os demais usa um navegador comum. Nunca conecta em endereços internos da rede.
- Extrair conteúdo de uma plataforma pode violar os termos de uso dela. O projeto é para uso pessoal e a extração é opt-in: sem chave, ela não acontece. A responsabilidade do uso é sua.

## Operação

### Backup

O estado está em um arquivo SQLite. Use o `.backup` (seguro com o bot rodando) e copie para fora da VPS:

```bash
docker compose exec guardei sqlite3 /data/app.db ".backup /data/backup.db"
docker compose cp guardei:/data/backup.db "./backup-$(date +%F).db"
```

### yt-dlp

Ele quebra sempre que uma plataforma muda. O bot roda `yt-dlp -U` toda semana enquanto estiver no ar. Numa atualização da imagem, use `docker compose build --pull` e, se precisar de uma versão específica, `--build-arg YTDLP_VERSION=<versão>`.

### Recursos

Medidos com o container limitado a 512 MB:

| | Medido |
| --- | --- |
| Imagem, com `yt-dlp` e `ffmpeg` | ~199 MB |
| RAM do bot ocioso ou buscando | 8 a 15 MB |
| Pico durante uma transcrição | ~100 MB de processos (bot + `yt-dlp` ~81 MB, ou `ffmpeg` ~69 MB) |
| Pico do container, contando cache de arquivos | 183 MiB, sem estourar o limite |
| Construir a imagem | precisa de ~1 GB de RAM; com 512 MB só com swap (45 s no meu teste) |

Uma busca leva cerca de 0,3 s, quase toda na chamada ao Gemini para entender o texto; a comparação com milhares de itens leva alguns milissegundos (7 ms com 10 mil itens, 41 ms com 50 mil, medidos). "Ver mais" e os botões de período respondem em 1 ms.

Só há uma extração de áudio por vez, e `yt-dlp` e `ffmpeg` rodam em sequência, por isso o pico não soma os dois. O uso normal é de poucos MB, mas uma extração passa dos 100 MB por alguns segundos. **Rodar** cabe em 512 MB; **construir** a imagem é que pede mais memória.

## Desenvolvimento

```bash
gofmt -l .                       # deve listar nada
go vet ./... && go vet -tags live ./...
go test -race ./...
```

Os testes com serviços reais (Gemini, `yt-dlp`, sites) só rodam com a tag `live`, para nunca acontecerem por acidente:

```bash
set -a; . ./.env; set +a
go test -tags live ./internal/ai -run Live -v            # Gemini: texto e áudio
LIVE_VIDEO_URLS="https://youtu.be/..." LIVE_QUERIES="termo;outro termo" \
  go test -tags live ./internal/bot -run LiveVideos -v   # vídeo ponta a ponta
LIVE_POST_URLS="https://x.com/..." \
  go test -tags live ./internal/bot -run LivePosts -v    # posts e páginas
```

Estrutura:

```
cmd/bot/            ponto de entrada
internal/bot/       conversa: handlers, comandos, fluxo de links e posts
internal/platform/  detecção de plataforma e URL canônica
internal/extract/   yt-dlp + ffmpeg (download, conversão, fatiamento)
internal/page/      leitura do texto público de posts e páginas (Open Graph e HTML)
internal/ai/        interface Client, Gemini e a versão sem IA
internal/store/     SQLite, migrações, FTS5
internal/search/    FTS, cosseno, RRF e a interpretação de períodos e plataformas
migrations/         SQL aplicado na inicialização
```

Não há mocks de rede: os testes de `internal/bot` usam dublês das interfaces `ai.Client`, `extract.Extractor` e `page.Reader`, e os de `internal/ai` e `internal/page` sobem servidores HTTP locais.

## Licença

[MIT](LICENSE)
