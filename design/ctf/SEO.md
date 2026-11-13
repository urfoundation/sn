# Capture the flag: SEO and agent findings

Research of 2026‑11‑11 for ur.xyz `/ctf` and its planned country pages (`CAPTURETHEFLAG.md` §9).

- **Method.** Three parallel passes covered search demand, Google's policies and the compliance limits, and
  AI search and agents. Sources are linked inline.
- **Data limits.** Search volumes are third‑party estimates. Google Trends refused automated access, so there
  are no first‑hand Trends comparisons.
- **The design.** The page is `design.html`, beside this file. Open it in a browser; `?page=ke` shows a
  country page.

## Summary

1. **"Capture the flag" must never lead a title.** In search it means hacking contests and the playground
   game ([Wikipedia](https://en.wikipedia.org/wiki/Capture_the_flag), [CTFd](https://ctfd.io/v1/whats-a-ctf)).
   Lead with plain earning words, "earn crypto by sharing your internet", and keep "Capture the flag" as the
   program's name.
2. **Of the three seed patterns, one fits well, one partly and one badly.** Each country page answers all
   three, in the searcher's own words, under question headings, with honest answers (§2.2). The titles and
   H1s use "earn crypto".
   - "Make money (online) in <country>" is the best fit.
   - "Passive income in <country>" fits in emerging markets, but it is a phrase that regulators pursue.
   - "Remote work in <country>" means jobs, and a page that is not a job must say so.
3. **Only country pages can rank for "<country>" searches.**
   - The hub's "Your country" card follows the visitor's IP, and Googlebot crawls mostly from the US.
   - `/ctf/<cc>` pages rank only if each carries its own data, not a swapped name. Otherwise they are doorway
     or scaled‑content abuse, and that penalty can be site‑wide.
   - So a country page that differs from another only by the country's name is `noindex` until its data or a
     local note sets it apart (§2.1).
4. **Every number has to be in the HTML.** Astro renders the pages statically from a per‑epoch data snapshot,
   and React islands only add interaction. Most AI crawlers run no JavaScript.
5. **This is YMYL money content, so trust decides.**
   - Real payout data and dates.
   - Disclosures beside the numbers.
   - No fiat projections.
   - "Not a job, not an investment".
   - The legal risks of §3 are accepted: the owner passed on counsel's review before launch (2026‑11‑11).
     They are the UK and EU crypto marketing rules, how capture the flag (an auction) is classified,
     sanctions, and extenders in censored countries.
6. **Agents need data, not tricks.**
   - Per‑country JSON with units and `as_of`, the OpenAPI spec and open CORS. No MCP server (owner decision,
     §8).
   - `llms.txt` helps agents, not citations.
   - ur.xyz's robots.txt already welcomes AI crawlers.
7. **AI answers favour other sites over a brand's own pages.** Directories, comparison lists, local tech press
   and video walkthroughs move citations more than on‑page wording.

## 1. What people search for

### 1.1 The seed patterns

| Pattern | What ranks today | Fit | How the page uses it |
|---|---|---|---|
| make money (online) in <country> | Local media and fintech blogs ([Zikoko](https://www.zikoko.com/money/how-to-make-money-online-in-nigeria-in-2025/), [Kenyans.co.ke](https://www.kenyans.co.ke/news/42763-how-easily-make-money-online-kenya), [Moneymax](https://www.moneymax.ph/blog/easy-ways-to-earn-money)) and vendor country pages ([Honeygain Pakistan](https://www.honeygain.com/blog/how-to-make-money-online-in-pakistan/), [Pawns Egypt](https://pawns.app/how-to-make-money-online-in-egypt/)) | High | The first question heading on each country page, answered with that country's numbers |
| passive income in <country> | Banks and investment content ([KCB](https://ke.kcbgroup.com/creating-passive-income-a-necessity-for-overcoming-economic-turbulence), [Kuda](https://www.kuda.com/blog/here-is-how-to-build-passive-income-in-nigeria/), [N26](https://n26.com/de-de/blog/passives-einkommen)); Honeygain ranks for "passive income in Nigeria online" | Medium in emerging markets, low in high‑income ones | A question heading: it runs in the background, but earns only while online, and is not an investment |
| remote work in <country> | Hiring and employer‑of‑record vendors ([Rivermate](https://www.rivermate.com/guides/kenya/remote-work), [Oyster](https://www.oysterhr.com/remote-work/germany)), law firms, job boards | Low: job intent | A question heading answered "No, it is not a job", in body text only |

**Demand signals** (estimates):

- **Nigeria.**
  - Google says "How to make money online?" was Nigeria's second most asked question over 15 years
    ([Legit.ng](https://www.legit.ng/1428906-google-trends-turns-15-reveals-nigerias-top-searches-over-past-decade-a-half.html)).
  - "How to make money with AI" rose 40% in 2025 ([Techeconomy](https://techeconomy.ng/nigerians-search-on-google-how-to-make-money-with-ai-new-trends-reveal/)).
- **The category is South and Southeast Asian.** Honeygain's organic traffic in October 2026 came from India
  18.5%, Nepal 11.3%, the US 7.3%, Indonesia 6.9% and Vietnam 5.2%. Pawns.app's came from India 39.8%,
  Indonesia 20.8% and the US 8.2% ([Ahrefs](https://ahrefstop.com/websites/honeygain.com),
  [Ahrefs](https://ahrefstop.com/websites/pawns.app)).
- **US monthly volumes** from Google Ads data ([seodata.dev](https://www.seodata.dev/keyword/bandwidth-sharing-apps)):
  - "passive income apps": 1,600;
  - "bandwidth sharing": 110;
  - "sell unused bandwidth": 30, at a $72 CPC.

  US demand is in brand and "alternative" searches, not in the category words.
- **Brands:**
  - "honeygain" was 13K a month in the US;
  - "earnapp" 90.5K a month, up 120%;
  - "DePIN" 8.1K a month, up 58%
  ([Ahrefs](https://ahrefstop.com/websites/honeygain.com), [Exploding Topics](https://explodingtopics.com/topic/earnapp),
  [Exploding Topics](https://explodingtopics.com/topic/depin)).

### 1.2 Related terms

- **High intent:**
  - "apps that pay you to share internet";
  - "sell unused bandwidth" or "sell internet data";
  - "bandwidth sharing app";
  - "get paid to share internet";
  - "passive income app".
- **Payout and currency** words, heavily targeted by spam:
  - "app that pays to M‑Pesa", "GCash", "Easypaisa / JazzCash", "bKash", "payée en FCFA";
  - "earn dollars online in Nigeria";
  - "ganar en dólares desde casa";
  - "internetten dolar kazanma";
  - "کسب درآمد دلاری".
- **Crypto and nodes:**
  - "earn crypto without investment";
  - "run a node, earn crypto";
  - "DePIN apps that pay daily";
  - "dVPN node earnings";
  - "mine Bittensor without a GPU".
- **Extender angle:**
  - "help people in Iran access the internet";
  - "get paid to fight censorship";
  - "Psiphon Conduit alternative". Conduit leads with "Be the reason someone can connect." and now shows
    rewards ([Conduit](https://conduit.psiphon.ca/en)).
- **Trust:**
  - "is bandwidth sharing safe" ([Trend Micro on proxyware](https://www.trendmicro.com/en_gb/research/23/b/hijacking-your-bandwidth-how-proxyware-apps-open-you-up-to-risk.html));
  - "is URnetwork legit";
  - "how much can you earn sharing internet in <country>".

### 1.3 Local languages

| Market | Terms seen ranking (no volumes found) |
|---|---|
| Spanish | ganar dinero desde casa en <país>, aplicaciones para ganar dinero sin invertir, ganar dinero compartiendo internet, ingresos pasivos |
| Portuguese (BR) | renda extra, renda passiva, como ganhar dinheiro na internet, vender dados da internet |
| Indonesian | cara dapat uang dari internet tanpa modal, aplikasi penghasil uang tanpa deposit |
| Vietnamese | kiếm tiền online tại nhà, app kiếm tiền chia sẻ internet, thu nhập thụ động |
| Turkish | internetten para kazanma, evden para kazanma, internet paylaşarak para kazanma |
| Russian, Ukrainian | заработок в интернете без вложений, пассивный доход; заробіток в інтернеті без вкладень |
| Arabic | الربح من الانترنت بدون رأس مال، بيع بيانات الإنترنت |
| Persian | کسب درآمد از اینترنت، کسب درآمد دلاری; the VPN word is فیلترشکن |
| Hinglish (romanized) | online paise kaise kamaye, mobile se paise kaise kamaye, paise kamane wala app |
| French (Africa), Swahili, Bengali | gagner de l'argent en ligne au <pays>; jinsi ya kutengeneza pesa mtandaoni; অনলাইনে টাকা ইনকাম |

- **India searches in romanized Hindi.** Honeygain's Hindi site is in romanized Hinglish, not Devanagari:
  "Passive Income - Bina Mehnat ke Kamaai" ([honeygain.com/hi](https://www.honeygain.com/hi/)).
- **ur.xyz's locales.** It serves en, ru, ar, zh, de and es, and capture the flag uses those only: no new
  locales (owner decision, 2026‑11‑11). Spanish, Russian and Arabic cover large parts of this demand.
  Portuguese, Indonesian, Vietnamese, Turkish, Hindi and French would have been new locales.

### 1.4 Competitors

- **Pawns.app (IPRoyal)** has at least 109 country pages on one template, "How to Make Money Online in
  <Country> 2026 Guide | Pawns" ([sitemap](https://pawns.app/page-sitemap.xml)).
  - Only the name changes between pages, along with one generic "$5 up to $140 a month" claim.
  - That template is the pattern to avoid.
- **Honeygain** writes its country posts by hand, each with local payout methods and currency
  ([Nigeria](https://www.honeygain.com/blog/how-to-make-money-online-in-nigeria/)).
  - It has landing pages per intent: `/sell-internet-data/`, `/passive-income-app/`, `/make-money-online/`.
  - It runs competitor posts ("apps like Pawns.app", "EarnApp alternatives").
- **Grass, Mysterium, Repocket and PacketStream** have no country pages. Grass lists the jurisdictions it
  excludes ([FAQ](https://grass-foundation.gitbook.io/grass-docs/introduction/general-grass-faq)).
- **URnetwork is already listed** on [AlternativeTo](https://alternativeto.net/software/urnetwork/about/) and
  [DePINHub](https://depinhub.io/projects/urnetwork/). That makes "URnetwork review", "URnetwork vs Honeygain"
  and "Honeygain alternatives" winnable.
  - Those would need a separate, sourced comparison page, not `/ctf`. The owner decided against one
    (2026‑11‑11), so they are not pursued.

### 1.5 How people ask assistants

Most assistant prompts match no keyword: 65–85% of ChatGPT prompts are absent from Semrush's 27‑billion‑term
database ([Semrush](https://www.semrush.com/blog/chatgpt-search-insights/)). The page should answer these
questions in plain sentences:

1. How can I make money online in Kenya, Nigeria or Ghana with just my phone or home internet?
2. Which apps pay for unused internet, and how is URnetwork different?
3. Is URnetwork legit? How can I check that people get paid?
4. How much can I realistically earn in my country, and how often?
5. What do "open slot", "pot" and "chance to win" mean, and how are winners chosen?
6. Which country has the biggest open pot right now? Does mine have open slots?
7. Provider or extender: what is the difference, and which should I run?
8. What do I need? Does it cost money, use my mobile data or drain my battery?
9. Is it safe and legal to share my connection? Will my ISP mind?
10. How do I get paid, and can I cash out locally?
11. Do I need a crypto wallet or any Bittensor knowledge? What is subnet 25?
12. Is it passive income? Is it a job?
13. Is running an extender risky in a censored country?
14. How fresh is this data, and can my AI assistant read it?

Agents acting for someone ask for tasks:

- "find the country with the biggest open pot";
- "is there an open provider slot in Ghana? set me up";
- "tell me when Nigeria opens".

The API answers those (§5.3).

## 2. The pages

### 2.1 Google's rules for 249 country pages

- **Doorways and scaled content.**
  - Pages "created to rank for specific, similar search queries" are doorways, and region pages that funnel
    to one page are Google's own example ([spam policies](https://developers.google.com/search/docs/essentials/spam-policies)).
  - Since March 2024, scaled content abuse covers pages made at scale mainly to rank, whoever writes them
    ([post](https://developers.google.com/search/blog/2024/03/core-update-spam-policies)).
  - Site‑wide classifiers exist ([FAQ](https://developers.google.com/search/help/helpful-content-faq)), so a
    bad template can sink the hub too.
- **What passes: data pages.** Wise's currency pages, Numbeo's cities and Nomads.com's cities all carry live
  figures, history, sources, an update date and links to neighbours
  ([Wise](https://wise.com/gb/currency-converter/usd-to-eur-rate), [Numbeo](https://www.numbeo.com/cost-of-living/in/Berlin),
  [Nomads](https://nomads.com/berlin)). Each CTF country page should have:
  - this epoch's slots, pots and chance to win;
  - the last epoch's winners and units;
  - the pot's history since `E_c`;
  - the realized rewards: the share of entries that won, and the median;
  - a local note: how to claim and cash out there, and what local law says, dated and reviewed by a person;
  - links to the hub and to neighbouring countries.
- **Near‑duplicates are merged.** Google indexes one page of a cluster whose main content is "very similar"
  ([canonicalization](https://developers.google.com/search/docs/crawling-indexing/canonicalization)). A
  title that differs "by only a single piece of information" counts as boilerplate
  ([title links](https://developers.google.com/search/docs/appearance/title-link)). Data‑driven
  descriptions are encouraged ([snippets](https://developers.google.com/search/docs/appearance/snippet)).
- **What to index: every page with its own data, from launch** (owner decision, 2026‑11‑11, made once the
  pages were built).
  - The hub, and every country page with its own data or a local note, are indexable from launch.
  - A country page that differs from another only by the country's name is `noindex` until its data or a
    local note sets it apart. It is still built, linked from the hub and served, but it is left out of the
    sitemap. The rule is applied afresh to each epoch's data, so a page becomes indexable with the first
    epoch whose data, or the first local note, makes it its own.
  - Two pages differ only by name when what each says of its own country is the same once the country's
    names and code are masked: the page's text without its neighbours, its other big pots and the days it
    states. The site's audit reads the built pages the same way, so no two indexable country pages differ by
    the name alone (§6).
  - Before launch that is 109 of the 249 pages, in 10 groups: the 75 inhabited countries the network's
    dashboard never counted; the seven territories with no permanent population (AQ, BV, GS, HM, IO, TF,
    UM), which read alike while nobody runs there; and 27 countries the dashboard counted alike, such as
    Kenya and Liechtenstein.
  - The rule replaces the decision earlier the same day to index all 249 pages, which would have indexed
    those 109 too.
  - Pages for countries under comprehensive sanctions, and for countries where circumvention tools are
    illegal, follow the same rule. The owner accepted their risks and passed on counsel's review (§3).
  - The weight falls on each page's own data from its first epoch.
- **The local notes, in order.** Write them for the demand markets of §1.1 first, plus the US, UK, Canada and
  Germany, where Honeygain says bandwidth is worth most
  ([Honeygain](https://www.honeygain.com/blog/bandwidth-sharing-apps/)):
  - India, Indonesia, Nigeria, the Philippines, Pakistan, Vietnam, Bangladesh, Nepal;
  - Kenya, Ghana, Egypt, Brazil, Mexico, Turkey.

### 2.2 Titles, headings and questions

The site's audit (`astro/scripts/seo-audit.mjs`) caps titles at 60 graphemes and requires unique titles and
descriptions per language, so the templates are sized for it.

**Hub, `/ctf`**

- **Title:** "Earn crypto by sharing your internet in 249 countries | UR" (58).
- **Description, from data:** "Five provider and five extender reward slots in every country, every week.
  This epoch: {openP} countries with open provider slots and {carried} α in pots."
- **H1:** "Capture the flag", the program's name, with the mission line under it.
  - The first sentence of the intro gives the plain answer: "Run the free URnetwork app as a provider or an
    extender and you can win weekly crypto rewards wherever you are."
- **Below the grid:** the questions of §1.5 that apply to every country.

**Country, `/ctf/<cc>`**

- **Title:**
  - "Earn crypto in {Country}: open slots and pots | UR" when it fits;
  - else "Earn crypto in {Country} | UR";
  - else a short name: "the DR Congo", "South Georgia".
- **Description, from data:** "{Country} this epoch: {po} provider and {xo} extender slots open, with pots of
  {ppot} α and {xpot} α. Stay online for half the epoch to win. Updated {date}."
- **H1:** "Earn crypto in {Country} with your internet connection".
- **Question headings, in the searchers' words:**
  1. "Can you make money in {Country} with URnetwork?": yes, in small amounts, followed by the slot amount,
     the pots, last epoch's winners and the disclosure.
  2. "Is it passive income?": it runs in the background, but earns only while it stays online. Amounts change
     with competition and alpha's price, and it is not an investment.
  3. "Is it remote work in {Country}?": no. It is not a job: no employer, hours or wage. It can run alongside
     remote work or any other job, on a connection you already have.
  4. "Provider or extender: which should I run in {Country}?": whichever role has open slots, from the data.
  5. "How are winners chosen, and can I check?": the census is committed on chain, then a drand round draws.
     With five entries or fewer, everyone wins.
  6. "How do I get paid?": to the payout wallet the app already uses, claimed with pool earnings in the app.
     The local note covers cashing out.

These headings carry the three seed patterns honestly. The title stays clear of "make money", "passive
income" and "remote work", because those words are earnings or employment claims (§3.1) and would mislead a
page that pays a few tenths of an α.

### 2.3 Copy rules

- **Always:**
  - amounts in α, with the epoch and date in the same sentence ("As of epoch 8, 2026‑12‑04, …");
  - "epoch (one week)" on first use;
  - the realized results beside any amount: what share of entries won, and the median.
- **Never:**
  - fiat projections, "up to $X", lifestyle images or top‑earner testimonials;
  - urgency built from stale numbers;
  - an invitation to buy, hold or stake alpha.
- **Words to avoid:** guaranteed, risk‑free, secure or protected returns, get rich, easy money, financial
  freedom, quit your job, job, hiring, salary, wage, investment, returns, yield, APY, lottery, jackpot, bet.
  - "Only N slots left" may appear only when N is live and true.
- **Wording to keep.** The owner chose "chance to win", "pot" and "draw" (§12.1, item 15), and calls capture
  the flag an auction (2026‑11‑11). Next to them, the page says how the draw works, and that taking part costs
  nothing beyond running the app.
- **The disclosure, beside the numbers:**
  - Rewards are paid in alpha, the token of Bittensor subnet 25. Its value changes and can fall.
  - What you win depends on uptime, on how many others run in your country, and on the network's emission.
  - Last epoch in {Country}, {share} of entries won a slot.
  - It is not a job or an investment, and you pay your own costs, such as power and data.
  - Local law applies.

### 2.4 Structured data

| Type | Status | Use |
|---|---|---|
| Dataset | Dataset Search only since 2025‑11‑05; needs `name` and a 50–5,000‑character `description` ([doc](https://developers.google.com/search/docs/appearance/structured-data/dataset)) | Hub, with the JSON API as its `distribution` |
| WebPage, Country | Machine‑readable only | Country pages: `about` names the country by ISO code and Wikidata id |
| BreadcrumbList | Still shown, desktop only | `Base.astro` already builds it from `breadcrumbName` |
| FAQPage | Rich result removed 2026‑05‑07 ([changelog](https://developers.google.com/search/updates)) | Skip the markup; keep the questions as visible HTML |
| SoftwareApplication | Needs ratings that are not copied from stores ([doc](https://developers.google.com/search/docs/appearance/structured-data/review-snippet)) | Skip |
| JobPosting | "Promotional content disguised as a job posting" is not allowed ([doc](https://developers.google.com/search/docs/appearance/structured-data/job-posting)) | Never |

- Markup must match what the page shows ([policies](https://developers.google.com/search/docs/appearance/structured-data/sd-policies)).
- Keep fiat amounts out of JSON‑LD, descriptions and share images, where they would lose their disclosures.
- `design.html` carries example nodes for the hub and for a country page.

## 3. Compliance

These are the legal risks the research found. The owner accepted them and passed on counsel's review before
launch (2026‑11‑11), so none holds up launch and no legal question here stays open. They are described so a
reader knows what was accepted; the copy rules of §2.3 still apply.

### 3.1 Earnings and employment claims

- **United States.** The FTC's notice on money‑making opportunities treats these as deceptive:
  - implying participants will earn "any specific amount";
  - earnings figures without typical results, time and costs;
  - false regional scarcity;
  - "must act immediately";
  - "risk‑free";
  - misrepresenting "the position being offered".

  The source is the [FTC notice](https://ftc.gov/system/files/attachments/penalty-offenses-concerning-money-making-opportunities/mmo-notice.pdf).
  Civil penalties are $53,088 per violation. "Passive income" draws enforcement: in April 2026 Publishing.com
  paid $1.5M over a "foolproof, passive income system"
  ([FTC](https://www.ftc.gov/news-events/news/press-releases/2026/04/publishingcom-pay-15-million-misleading-consumers-about-how-much-income-they-could-earn-using)).
- **The Business Opportunity Rule** requires a payment to the seller, so a free app likely falls outside it
  ([16 CFR 437.1](https://www.law.cornell.edu/cfr/text/16/437.1)). The 2025 proposal to extend it to
  money‑making opportunities has no final rule that we could find.
- **"Remote work"** reads as a job offer. Three things weigh against using it as a title: the FTC's "position
  offered" item, the rater guidelines' "misleading title" (rated Lowest), and Google's ban on disguised job
  posts. Use it in body text only, to say "this is not a job".

### 3.2 Crypto promotion

- **United Kingdom.** Since 2023‑10‑08 a cryptoasset "financial promotion" to UK consumers needs an
  authorised route. This applies to overseas firms too ([FCA](https://www.fca.org.uk/firms/cryptoassets/marketing-uk-consumers),
  [FG23/3](https://www.fca.org.uk/publication/finalised-guidance/fg23-3.pdf)). Whether earning alpha for
  bandwidth is "investment activity" is unresolved. Either way the pages never invite anyone to buy, hold or
  stake alpha.
- **European Union (MiCA).**
  - Article 7 requires marketing to be fair, clear and not misleading, and to state that it was not reviewed
    by a competent authority.
  - Article 4(3) exempts crypto‑assets offered for free or created as a reward for maintaining the ledger
    ([ESMA](https://www.esma.europa.eu/publications-and-data/interactive-single-rulebook/mica/article-4-offers-public-crypto-assets-other)).
  - Whether URnetwork is an "offeror" of alpha was left to counsel; the owner accepted it unreviewed.
- **India.** The ASCI code requires "Crypto products and NFTs are unregulated and can be highly risky" on
  crypto ads.
- **Google Ads** bans "get rich quick", guaranteed returns and risk‑free framing
  ([policy](https://support.google.com/adspolicy/answer/15936857)). Node rewards are not addressed.

### 3.3 The auction, sanctions and safety

- **An auction.** Capture the flag is an auction of each country's slots every epoch (owner decision,
  2026‑11‑11). Running the app is its only entry condition. How it is classified in each market was left to
  counsel; the owner passed on that review and accepted the risk, so no legal‑classification question stays
  open.
- **Sanctions.** A US general license (GL D‑2) authorizes anti‑censorship services for Iran
  ([Federal Register](https://www.govinfo.gov/content/pkg/FR-2022-10-13/pdf/2022-22233.pdf)). Paying
  residents of sanctioned countries is a separate question, and the owner accepted it unreviewed: no country
  is excluded, and CTF payouts are screened by the SDN address list as every payout is (`CAPTURETHEFLAG.md`
  §7). Those pages are not held back; they follow §2.1's rule like every other.
- **Extenders in censored countries.** Freedom House found anti‑censorship tools blocked in at least 21 of
  72 countries ([Tunnel Vision](https://freedomhouse.org/report/special-report/2025/tunnel-vision-anti-censorship-tools-end-end-encryption-and-fight-free)).
  Recruiting extenders there carries legal and safety risk for them. The owner accepted it without a legal
  review. A dated safety note on such a page, like a local note, would warn them; none is written yet.
- **App stores.**
  - Apple's guidelines bar crypto apps from offering currency for completing tasks.
  - Google Play allows proxy services only as an app's core purpose.

  The store copy of the provider apps should match how `/ctf` describes the rewards
  ([Apple](https://developer.apple.com/app-store/review/guidelines/), [Play](https://support.google.com/googleplay/android-developer/answer/9888379)).

### 3.4 Trust signals

Money pages without responsible contact information are rated Lowest
([rater guidelines §4.5.1](https://static.googleusercontent.com/media/guidelines.raterhub.com/en//searchqualityevaluatorguidelines.pdf)).
The pages link:

- who runs the program, with contact details;
- the method (`CAPTURETHEFLAG.md` §4 and §8, or a page made from them);
- the on‑chain census and drand round of every draw;
- the open‑source code;
- a note that the country pages are generated from data and reviewed by people.

## 4. AI search

- **Fundamentals still decide.**
  - Google's AI Overviews and AI Mode use its core ranking, and fan one question out into many searches. A
    page only needs to be indexed and eligible for a snippet. Google calls llms.txt, chunking and AI‑specific
    rewrites myths, and warns that mass‑producing pages for fan‑out queries is scaled‑content abuse
    ([guide](https://developers.google.com/search/docs/fundamentals/ai-optimization-guide),
    [post](https://developers.google.com/search/blog/2026/05/a-new-resource-for-optimizing)).
  - Search Console's generative‑AI setting stays on "Include".
- **What the research shows.**
  - The GEO paper found that adding statistics, quotations and cited sources raised visibility by up to 41%
    in a simulated engine. Keyword stuffing lowered it ([arXiv 2311.09735](https://arxiv.org/abs/2311.09735)).
  - A 2025 benchmark found most such tactics "largely ineffective" on live systems
    ([C‑SEO Bench](https://arxiv.org/abs/2506.11097)).
  - AI search is biased toward earned, third‑party media ([Chen et al.](https://arxiv.org/abs/2509.08919)).
  - AI‑cited pages are about 26% fresher than organic results
    ([Ahrefs](https://ahrefs.com/blog/do-ai-assistants-prefer-to-cite-fresh-content/)).
- **What follows for `/ctf`.**
  - Dated statistics in plain sentences: "As of epoch 8, Kenya had 3 open provider slots and a 1.24 α pot."
  - Consistent names: URnetwork, UR, alpha, subnet 25.
  - The answer near the top.
  - Coverage on other sites: DePIN directories, comparison lists, African and Asian tech press, YouTube
    walkthroughs.
- **Meta AI in WhatsApp** answers from live Bing and Google results in Nigeria, Ghana and Kenya
  ([Techpoint](https://techpoint.africa/news/meta-ai-enables-google-bing-searches/)). Meta‑WebIndexer feeds
  its citations ([Meta](https://developers.facebook.com/docs/sharing/webmasters/web-crawlers)).
- **The litepaper contradicts CTF.** `docs/litepaper.md` says of the provider pool: "There is no slot to win
  and nothing to burn." An assistant may merge the two programs. When CTF launches, that line should say the
  pool has no slot to win, and that capture the flag, beside it, has ten per country.

## 5. Agents

### 5.1 robots.txt

`astro/public/robots.txt` already allows everyone and names 18 AI agents.

- **Add `Meta-WebIndexer`**, for the WhatsApp markets.
- **Optionally add a `Content-Signal: search=yes, ai-input=yes, ai-train=yes` line**
  ([Cloudflare](https://blog.cloudflare.com/content-signals-policy/)). It is voluntary.
- **Check the CDN.** Cloudflare's managed robots.txt prepends its own blocks of GPTBot, ClaudeBot,
  Google‑Extended and others, and new zones have blocked AI crawlers by default since July 2025
  ([docs](https://developers.cloudflare.com/bots/additional-configurations/managed-robots-txt/)).
- **Let signed agents through.** ChatGPT's agent signs its requests (RFC 9421), so bot protection should not
  block it ([Cloudflare](https://blog.cloudflare.com/signed-agents/)).
- **Keep training crawlers allowed.** Only about a third of ChatGPT prompts used search in February 2026
  ([Semrush](https://www.semrush.com/blog/chatgpt-search-insights/)); the rest are answered from what the
  model already knows.

### 5.2 llms.txt

- **What it is worth.**
  - 97% of llms.txt files got no requests in May 2026, and search bots barely fetch them
    ([Ahrefs](https://ahrefs.com/blog/llmstxt-study/)).
  - Coding and browser agents do read them.
  - Lighthouse's experimental Agentic Browsing category checks for one
    ([Chrome](https://developer.chrome.com/docs/lighthouse/agentic-browsing)).
- **The generator would list every country page.** `scripts/generate-agent-assets-llms.mjs` lists every
  indexable English page. With 249 country pages that would break the 16 KiB budget that `seo-audit.mjs`
  enforces.
  - List the hub only, under a "Capture the flag" heading, with the JSON API and `sn/api/operator.yml`.
  - Leave the country pages out.
  - Optionally publish `/ctf/llms.txt`, which llmstxt.org v2 allows at a sub‑path
    ([llmstxt.org](https://llmstxt.org/)).

### 5.3 The API for agents (`CAPTURETHEFLAG.md` §9.1)

- **Answer the common questions directly.** Beside `/sn/ctf`, add `/sn/ctf/country/{cc}` (ISO 3166‑1
  alpha‑2) and `/sn/ctf/top?role=&limit=`.
- **Field conventions:**
  - `as_of` in RFC 3339 UTC, beside the epoch;
  - amounts as decimal strings in α, plus integer rao;
  - a unit object (token alpha, netuid 25);
  - chance to win as a 0–1 fraction, with a link to its definition;
  - slots as total, won and open;
  - no "1.2k" shorthand or locale separators;
  - errors in RFC 9457 form.
- **Headers:** `Access-Control-Allow-Origin: *`, a short `Cache-Control`, `ETag`, `Last-Modified`, and
  `Link: rel="service-desc"` to the OpenAPI document.
- **Discovery.** List the OpenAPI document in `/.well-known/api-catalog` ([RFC 9727](https://www.rfc-editor.org/rfc/rfc9727)).
  On each country page, set `Base.astro`'s `alternate` to that country's JSON.
- **MCP: none** (owner decision, 2026‑11‑11). ur.io's MCP server needs OAuth and an account. A read‑only,
  no‑sign‑in MCP server with `list_countries`, `get_country` and `top_open_pots` was considered and is not
  built; agents read the JSON API.
  - Nothing shows assistants discover such servers on their own; it would have served only people who add it.
- **Browser agents.**
  - Semantic buttons and links, labelled inputs and no invisible overlays
    ([web.dev](https://web.dev/articles/ai-agent-site-ux)).
  - The design's tap targets are real `<button>`s with labels.
  - Its country names are real links.

## 6. Building it in web/ur.xyz

The page's markup and behaviour are React (`react/src`), rendered to static HTML by Astro (`astro/`), where
every SEO concern lives. `/mainnet` is the model: `astro/src/pages/mainnet.astro` renders
`react/src/components/pages/Mainnet.jsx` and hydrates only its islands.

**React (`react/src`)**

- **Pages.** `components/pages/Ctf.jsx` for the hub and `components/pages/CtfCountry.jsx` for a country,
  from `design.html`.
- **Islands.** These hydrate; everything else is static:
  - the stats filters;
  - sorting and the search;
  - the tooltips;
  - tap to expand;
  - Shuffle;
  - the visitor's country card, from `/sn/ctf/here`.
- **Data.** A per‑epoch snapshot, `data/ctf-epoch.json`, written by an import script from `/sn/ctf`, as
  `scripts/research-report-import.mjs` does for the report. A test pins its invariants:
  - 249 codes;
  - pots ≥ 0;
  - winners ≤ 5 per role;
  - a sample's sum.
- **A deterministic static render.**
  - Easy wins is seeded by the epoch.
  - The country card starts neutral (the biggest open pot) until the island knows the visitor.
  - Nothing else varies by build.

  The page's fingerprint, and so its `lastmod`, then change only when the data does.
- **Lean HTML.** `design.html` renders about 945 KB for 249 cells, because every cell carries its hover and
  tap text.
  - The static page carries only each cell's link, flag, pots, slot status and chance to win.
  - The island writes the next‑step text when a card opens.

**Astro (`astro/`)**

- **Routes.** `src/pages/ctf.astro`, and `src/pages/ctf/[cc].astro` with `getStaticPaths()` over the 249
  codes. Paths are lowercase, in the clean form `publicPathFor` publishes, with no trailing slash (`/ctf/ke`).
- **`Base` props:**
  - `title` and `description` from §2.2's templates;
  - `breadcrumbName` ("Capture the flag", then the country);
  - `jsonLd` (Dataset on the hub; WebPage with `about` Country on country pages);
  - `alternate` (the country's JSON);
  - `noindex` per §2.1;
  - `ogImage` per epoch and country, named by epoch so caches refresh.
- **"Updated".** `LASTMOD_TOKEN` fills it, as on `/mainnet`, so the visible date, `dateModified` and the
  sitemap's `lastmod` agree.
- **Indexing.** `src/pages/ctf/[cc].astro` decides each country page's `noindex` in `getStaticPaths()` from
  the epoch's snapshot (§2.1). It renders every country page's body first and compares what each says of its
  own country (`react/src/components/pages/ctf/indexing.js`). The sitemap and `page-dates.json` leave the
  `noindex` pages out, as they leave out every `noindex` page.
- **Links.** Every cell of the 249‑country grid is a plain `<a href="/ctf/<cc>">`, so every country page is
  linked.
- **English first, like `/mainnet`.**
  - Localized versions, if any, use the site's current locales only (§1.3) and go through the shared
    localized‑route list.
  - hreflang links translations of one country page only, never two countries.
  - Never redirect by IP.
- **Gates.** `seo-audit.mjs` already checks:
  - title length;
  - unique titles and descriptions;
  - that the sitemap and canonicals agree;
  - hreflang;
  - the agent‑file budgets.

  Add one check, by the build's own comparison: every country page follows §2.1's rule, so no two indexable
  country pages differ by the country's name alone.
- **The rebuild.** After each epoch's census and root, the import runs, the site rebuilds and deploys.
- **`SITEMAP.md`.** A Capture the flag section, like the Mainnet one.

## 7. Measurement

- **Google Search Console.**
  - Clicks from AI Overviews and AI Mode count under the Web search type.
  - The generative‑AI report shows impressions only
    ([Google](https://developers.google.com/search/blog/2026/06/gen-ai-performance-reports)).
  - Watch for duplicate‑cluster warnings on the country pages.
- **Bing Webmaster Tools.** Its AI Performance report shows Copilot citations and the grounding queries
  behind them ([Search Influence](https://www.searchinfluence.com/blog/bing-ai-performance-report-copilot-citations/)).
  It is the free view of what assistants actually search for.
- **Referrers:**
  - chatgpt.com (with `utm_source=chatgpt.com`);
  - perplexity.ai;
  - gemini.google.com;
  - copilot.microsoft.com;
  - claude.ai.

  Treat the counts as a floor, since app traffic often arrives without a referrer.
- **Server logs.** Each hit from ChatGPT‑User, Claude‑User, Perplexity‑User, Meta‑ExternalFetcher or
  Google‑Agent is an assistant reading the page for a person. Count those, and the API's clients.

## 8. The owner's decisions

The owner decided these on 2026‑11‑11, and no question for the owner stays open:

1. **Counsel's review: passed on.** The owner accepted the risks of §3 without a review before launch:
   - the UK and EU promotion rules;
   - how capture the flag (an auction) is classified;
   - sanctions;
   - extenders in censored countries.
2. **New locales: none.** The pages use the site's current locales only: en, ru, ar, zh, de and es (§1.3).
3. **The read‑only MCP server: not built** (§5.3).
4. **A comparison page** for "Honeygain alternatives" and "URnetwork vs …": not built (§1.4).
5. **Indexing.** Every country page with its own data or a local note is indexable from launch; a page that
   differs from another only by the country's name is `noindex` until it does not (§2.1).
