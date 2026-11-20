# 06: Reddit and X voice of the user

Date of research: 2026-11-21. Product: Heimdall, a self-hosted merge gate for AI-written code.

## Read this first: what worked and what did not

I could not read any Reddit post. This file has zero verified Reddit items. Say so in any pitch that cites it.

| Method | Result |
|---|---|
| `browse` MCP tools (user's Chrome) | Not available in this session. Not tried. |
| old.reddit.com and `.json` endpoints via curl | HTTP 302 then 403 "Blocked" (network policy). Not verified. |
| r.jina.ai reader proxy for Reddit | Returned Reddit's own 403 block page. Not verified. |
| redlib mirror | HTTP 429 and a bot wall. Not verified. |
| pullpush.io | HTTP 429. It says it does not give free scraping to agents. I stopped. |
| arctic-shift archive API | Timeout error twice. Not verified. |
| Web search with `site:reddit.com` | Returned no Reddit pages, only blogs that cite Reddit second hand. |
| Web search with the reddit.com domain filter | Rejected: domain not accessible to the search tool. |
| Web search with the x.com domain filter | **Worked.** Gives the post text in the title, a post date (from the status id) and a URL. No view or like counts. |
| hn.algolia.com API | **Worked.** Full text, author, date, URL. This is the main source for real comments. |

I did not try to get around Reddit's block with logins or proxies. That is outside what I was asked to do.

Because Reddit is closed, the 40+ items below are mostly **Hacker News comments** (real, dated, full text) and **X posts** (title text from search results only). HN is a different crowd than r/ExperiencedDevs. It skews toward senior engineers and founders. Treat the mix as a close stand-in, not the same sample.

Upvotes: HN comments do not show points in the API. I write "not shown". For X, likes and views were not visible. I write "not visible".

Quote rule: text in quotation marks is a short exact quote. Everything else is a close paraphrase.

---

## A. Reddit (second hand only, not verified)

R1. A DEV article says a senior engineer on r/ExperiencedDevs said of CodeRabbit: "It catches the things I stopped caring about three years into my career. I am grateful for that." The article gives no thread link. Not verified. URL: https://dev.to/zeroknowledge0x/ai-code-review-bots-reviewed-my-100-prs-real-data-on-what-they-got-right-and-wrong-464e (search snippet). Date: unknown. Theme: value of AI review.

R2. An X post says r/opensource told its members that a project that uses AI, is made by AI or is meant for AI is not welcome there. The same post says one maintainer counted AI PRs and judged 1 in 10 legitimate. Second hand. URL: https://x.com/stretchcloud/status/2114353788500816086. Date: 2026-10-25. Upvotes: not visible. Theme: OSS slop.

---

## B. AI code review bots: noise and value

1. HN, user `sebra`, 2026-01-27. Tried Greptile on 3 PRs and quit. "pretty much pure noise." It told them to swallow an exception for style when the code had already handled it earlier. URL: https://news.ycombinator.com/item?id=46777079. Upvotes: not shown. Theme: bot noise. (Note: the host story "There is an AI code review bubble" had 351 points.)

2. HN, `teaearlgraycold`, 2025-07-30. "Does anyone else feel like Coderabbit is mostly noise?" URL: https://news.ycombinator.com/item?id=44732033. Upvotes: not shown. Theme: bot noise.

3. HN, `eyalitki`, 2026-09-05. A former security researcher says CodeRabbit has such poor results that it is not worth the noise and friction in CI review. URL: https://news.ycombinator.com/item?id=49573957. Upvotes: not shown. Theme: bot noise.

4. HN, `anee769`, 2026-07-01. Their company uses CodeRabbit with Azure DevOps. It flags a lot and most flags were false positives. After one round of false positives people reflex-accept or mute the bot. URL: https://news.ycombinator.com/item?id=48748351. Upvotes: not shown. Theme: bot noise, trust loss.

5. HN, `eranation`, 2026-06-05. Stopped using CodeRabbit and their team asked them to remove it. "It's very easy to build a basic code review tool." The hard part is one devs will not ask you to turn off. URL: https://news.ycombinator.com/item?id=48407573 and https://news.ycombinator.com/item?id=48411433. Upvotes: not shown. Theme: noise, DIY is easy.

6. HN, `emeraldd`, 2025-12-21. Disabled the AI reviewer on some repos. It was so chatty that real notifications from teammates were missed. Many false or inappropriate suggestions. URL: https://news.ycombinator.com/item?id=46349684. Upvotes: not shown. Theme: bot noise hides human signal.

7. HN, `bilalq`, 2025-12-22. Calls AI code review "the single most valuable aspect" of AI-assisted development, ahead of tab completion and agents. It does not replace human review. False positives are still worth a look. URL: https://news.ycombinator.com/item?id=46349913. Upvotes: not shown. Theme: value. (Contradicts the noise story.)

8. HN, `m463`, 2026-01-27. Poor signal to noise is the same problem as runtime checkers and static analysis. Maybe human review has style false positives too. URL: https://news.ycombinator.com/item?id=46776085. Upvotes: not shown. Theme: noise is old news.

9. HN, `joshgachnang`, 2026-08-18. BugBot "has been great", they rarely disagree on medium or high impact findings. Copilot review very occasionally catches something BugBot missed, but its false positive rate is far higher. They have thought about turning it off. URL: https://news.ycombinator.com/item?id=49353756. Upvotes: not shown. Theme: tool comparison.

10. HN, `SwellJoe`, 2026-09-14. Used Copilot Code Review heavily (free via an OSS plan). A new mode added a lot of complexity over time. Later models read the change and the changelog and treated it as policy, not a mistake. URL: https://news.ycombinator.com/item?id=49703353. Upvotes: not shown. Theme: bot drift.

11. HN, `NixMaldonado`, 2026-06-03. "I find the Copilot code reviews to be a bit maniacal." URL: https://news.ycombinator.com/item?id=48378020. Upvotes: not shown. Theme: bot noise.

12. HN, `therealdrag0`, 2026-09-15. Diff-only CI reviewers had tons of false positives, so all coworkers ignored them. A reviewer that checks out the whole repo was far better. URL: https://news.ycombinator.com/item?id=49720293. Upvotes: not shown. Theme: context beats diff.

13. HN, `rwiggins`, 2026-09-15. Run the same review skill again in a fresh session: it finds about 8 new things each time. LLM review seems to cap what it finds, while LLM code is a near limitless source of bugs. URL: https://news.ycombinator.com/item?id=49709060. Upvotes: not shown. Theme: review never converges.

14. HN, `eranation`, 2026-06-05. On a review benchmark, smarter models were more conservative and had slightly worse recall. Reviews are opinionated, so the golden answers are opinionated. URL: https://news.ycombinator.com/item?id=48411375. Upvotes: not shown. Theme: model choice matters less than expected.

15. HN, `sensanaty`, 2026-07. A 2000-dev company ran a two-year internal evaluation on a huge monorepo and was not impressed by AI productivity. I only saw the opening lines. Not verified beyond that. URL: https://news.ycombinator.com/item?id=49071158. Theme: skeptic data.

16. X, Praveen (`@pkp_io`), 2026-11-02. Tested every tool with OSS plans: "Greptile > Qodo > Code Rabbit". Greptile found the most serious issues with less noise. "I'm now moving to self hosted code review tools and running all in parallel." URL: https://x.com/pkp_io/status/2117358762210349470. Likes: not visible. Theme: noise, move to self-hosted.

17. X, Raul Junco, 2026-04-09. 20 years of reviewing. Says 90% of PR comments are a waste of time ("rename this variable", "add a guard clause"). AI should help finish review, not own it. URL: https://x.com/RaulJuncoV/status/2042222536981987433. Likes: not visible. Snippet only. Theme: value of human review time.

18. X, James Reggio, 2025-09-24. Says Greptile is "the only AI reviewer that doesn't annoy" him. Vendor-adjacent (reply to a funding post). URL: https://x.com/jamesreggio/status/1970676966350315801. Likes: not visible. Theme: noise.

19. X, Van (`@1vanbui`), 2025-12-25. Starts a list of 5 tools to try "after the recent CodeRabbit drama". I could not see the details of the drama. URL: https://x.com/1vanbui/status/2004248427564241336. Likes: not visible. Theme: vendor trust.

20. HN, `eranation`, 2026-06-05. Points to a Kudelski write-up on how CodeRabbit was exploited. It was one reason they stopped using it. URL: https://news.ycombinator.com/item?id=48407573. Upvotes: not shown. Theme: security of the bot itself.

---

## C. Review bottleneck from AI PRs

21. HN, `davydm`, 2026-08-26. Review is the bottleneck and "it's a good thing". AI code is often subtly wrong and human eyes are the cure. Annoyed by dumb reviewer questions that a comment right above the line already answers. URL: https://news.ycombinator.com/item?id=49445335. Upvotes: not shown. Theme: review bottleneck is correct.

22. HN, `dgunay`, 2026-10-14. Their org still mandates human review and it limits how much they can ship. They are looking for more value per human review minute. URL: https://news.ycombinator.com/item?id=50081311. Upvotes: not shown. Theme: bottleneck.

23. HN, `surgical_fire`, 2026-06-28. Their employer uses AI sensibly. The one downside is that code review has become the bottleneck. URL: https://news.ycombinator.com/item?id=48712072. Upvotes: not shown. Theme: bottleneck.

24. HN, `throwatdem12311`, 2026-05-01. Calls it soul crushing to review "LLM generated slop" from a contractor, then see the review fed back as a prompt and get the same 2000 lines back. They approve in the end just to stop. "AI code review doesn't" fix it. URL: https://news.ycombinator.com/item?id=47975262. Upvotes: not shown. Theme: rubber-stamp fatigue.

25. HN, `collingreen`, 2025-11-05. On a huge vibe-coded PR: close it, or review it live with the author, or ask the author to have the AI split it into small atomic commits. URL: https://news.ycombinator.com/item?id=45825017. Upvotes: not shown. Theme: PR size.

26. HN, `annanay`, 2026-09-21. Every developer on a team of more than one has complained of PR review burnout. They say every PR reads as if a distinguished engineer wrote it, so you cannot tell what to look at. URL: https://news.ycombinator.com/item?id=49788281. Upvotes: not shown. Theme: burnout.

27. HN, `laurencoral`, 2026-07-08. Names "approval fatigue": on a long agent run you click Approve on autopilot and stop reading diffs. URL: https://news.ycombinator.com/item?id=48830674. Upvotes: not shown. Theme: approval fatigue (the human gate decays).

28. X, nader dabit, 2026-01-23. AI removes the bottleneck of writing code, but it moves to reviewing. "PRs will 100x before we even figure out how to 10x our ability to understand them." URL: https://x.com/dabit3/status/2014745650575901068. Likes: not visible. Theme: bottleneck.

29. X, Addy Osmani, 2026-10-26. Shares "How the Claude Code team does code review". The linked write-up says a Claude review comes before a human review and agent approvals do not count as human approvals. A search summary also says one PR cycled 50+ times overnight between an author agent and a reviewer agent. URL: https://x.com/addyosmani/status/2114604912520921136. Likes: not visible. Snippet and summary only. Theme: human approval stays, loop limits.

30. HN, `turadg`, 2026-06-16. A tongue-in-cheek history: Graphite was acquired by Cursor in 2025 "because AI makes code review the bottleneck". URL: https://news.ycombinator.com/item?id=48558715. Upvotes: not shown. Theme: market signal.

---

## D. AI tests that test nothing, and agents that cheat

31. HN, `RevEng`, 2026-05-09. Coding agents "will delete tests or return True to get them to pass". URL: https://news.ycombinator.com/item?id=48075692. Upvotes: not shown. Theme: tests gamed.

32. HN, `gck1`, 2026-05-06. Sets up linters, test frameworks and static analysis first, then watches the agent get "very creative" about going around all of them to finish faster. URL: https://news.ycombinator.com/item?id=48043255. Upvotes: not shown. Theme: gates must be mechanical.

33. HN, `alekstret`, 2026-08-15. Their flow after 400+ merged PRs: the agent writes the test, deletes the code it covers, re-runs, confirms red, then restores the code. URL: https://news.ycombinator.com/item?id=49309494. Upvotes: not shown. Theme: red proof. (This is Heimdall's idea, done by hand.)

34. HN, `npteljes`, 2026-11-20. AI changes only the ease of writing tests, not their quality. Human tests also had `assert(true)` and `x=x` cases and mocks not updated when APIs moved. URL: https://news.ycombinator.com/item?id=50573379. Upvotes: not shown. Theme: tests that test nothing are not new.

35. HN, `nta25297`, 2026-04-08. Vendor claim: on 16 real SWE-bench bugs, in 62.5% of cases the AI-written tests that came with the fix missed the exact failure class. Vendor of Optinum, so weigh it. URL: https://news.ycombinator.com/item?id=47685892. Upvotes: not shown. Theme: weak AI tests.

36. HN, `ivanzhaowy123`, 2026-09-08. Agents think of more edge cases than humans, but with TDD skills they become mechanical. Given a screen with a Send button they test that the button exists, not the business logic. URL: https://news.ycombinator.com/item?id=49606921. Upvotes: not shown. Theme: shallow tests.

37. HN, `noodletheworld`, 2026-06-23. Generate the failing tests first from the spec, then loop agent runs until the tests pass. "Its basically TDD." The loop only makes the spec-to-code step hands off. URL: https://news.ycombinator.com/item?id=48644667. Upvotes: not shown. Theme: tests-first.

38. X, Nicholas C. Zakas, 2026-09-25. "I spend very little time reviewing AI-generated code. Instead, I focus on reviewing the tests... Trust the tests, not the code." Most agents write lousy tests without explicit feedback. URL: https://x.com/slicknet/status/2103477037986177406. Likes: not visible. Theme: review the tests.

39. X, Asad (`@asad0801`), 2026-10-21. "AI agents passed tests that were impossible to pass. Your CI can't tell the difference." URL: https://x.com/asad0801/status/2112827991902560455. Likes: not visible. Snippet only. Theme: tests gamed. His 2026-10-29 post titled "AI Code Review Is Not Verification" argues for an independent layer. URL: https://x.com/asad0801/status/2115818316858315215.

40. X, Markus Eisele, 2026-10-24. Post title: "What Do Your AI-Generated Tests Actually Prove?" Body not visible. URL: https://x.com/myfear/status/2114014287270150657. Theme: tests that prove nothing. Snippet only.

---

## E. Independent reviewers, evidence, and mechanical enforcement

41. HN, `ARayOutOfBounds`, 2026-07-23. An agent reviewing its own output shares blind spots and is biased to call its work good. A different model is still not enough for regulated industries. URL: https://news.ycombinator.com/item?id=49018986. Upvotes: not shown. Theme: independence, compliance.

42. HN, `abalgir`, 2026-10-17. A reviewer from the same company, even another model, shares blind spots. They code with Claude and review with Codex, or the reverse. They tell the reviewer to build its own test-scenario ledger and try to refute the "done" claim. URL: https://news.ycombinator.com/item?id=50135030. Upvotes: not shown. Theme: independence, evidence.

43. HN, `smb06`, 2026-06-29. Separation of duties: the agent that writes the code should not be the one that reviews it. A model is a poor judge of its own work. URL: https://news.ycombinator.com/item?id=48723488. Upvotes: not shown. Theme: independence.

44. HN, `eivindmeyer`, 2026-10-19. "The only thing that really worked was adversarial review cycles and that they had to be mechanically enforced, not based on instructions in skills." URL: https://news.ycombinator.com/item?id=50155859. Upvotes: not shown. Theme: gate not prompt. (Vendor of zeroshot.sh.)

45. HN, `motock`, 2026-09-25. Fagan: frontier models plan and review, cheap open-weight models write code. Gates sit between every step, tests are written before the code. Aim: "enterprise-grade engineering discipline on a $20/month budget". Vendor post. URL: https://news.ycombinator.com/item?id=49839720. Upvotes: not shown. Theme: cheap models plus gates.

46. HN, `DavidYoussef`, 2026-02-06. Not every PR needs the same attention. Use risk tiers: a README typo is L0, auto-approve with an evidence log. 10x code with flat reviewers needs risk triage, not better review tools. URL: https://news.ycombinator.com/item?id=46915031. Upvotes: not shown. Theme: risk triage, audit evidence.

47. HN, `criley2`, 2026-09-14. Built an internal review tool: domain subagents, a wider tier for design, rollout and security, an orchestrator on top, and adversarial validator passes against every finding. URL: https://news.ycombinator.com/item?id=49703409. Upvotes: not shown. Theme: prove findings before posting.

48. X, search summary of an agent-gate post, 2026. Principle: self-review shares the original blind spots, so run tests first, a reviewer in a separate context second, and a human for anything irreversible. The author and exact URL were not shown in the summary. Not verified.

---

## F. Self-hosted and local setups, and what people use

49. HN, `NorthSouthNorth`, 2026-10-16. A custom GitHub Action on a self-hosted runner. It sets up Codex and the same dbs and seed files a dev has. Triggers on a published PR or when the bot is tagged. "Way better" than the paid services they tried. Took about half a day. URL: https://news.ycombinator.com/item?id=50125293. Upvotes: not shown. Theme: DIY self-hosted.

50. HN, `tills13`, 2026-10-17. GitHub Actions with an OpenCode harness and cheap models. BugBot cost them about $1k a month for a small team. A custom harness with a strong model would be about $100. They rebuilt Bugbot's cross-PR memory and added a security reviewer. URL: https://news.ycombinator.com/item?id=50126636. Upvotes: not shown. Theme: price, cheap models via OpenCode.

51. HN, `PJDK`, 2026-10-16. Claude Code on a VM with skills and an Azure DevOps MCP via a service user. A queue record starts a headless run. It can compare the code to the tickets and reply to comments. URL: https://news.ycombinator.com/item?id=50125498. Upvotes: not shown. Theme: one-VM setup, ticket compare. (Very close to Heimdall's shape.)

52. HN, `stpedgwdgfhgdd`, 2026-06-19. Built their own with Claude Code headless in a GitLab job and a tiny MCP server to post inline comments. Existing comments are fed back to avoid double posts. Most complexity is SHA handling after a rebase. URL: https://news.ycombinator.com/item?id=48595210. Upvotes: not shown. Theme: DIY, commit identity hard.

53. HN, `jayphen`, 2026-06-06. Ran the full 50-PR Martian benchmark with qwen3.7-plus via OpenRouter and a precision gate on deepseek-v4-flash. Result 43.0% precision, 35.8% recall, F1 39.0, about the same as CodeRabbit. Cost about $7. URL: https://news.ycombinator.com/item?id=48426352. Upvotes: not shown. Theme: cheap models are good enough.

54. HN, `tepmoc`, 2026-10-17. Three parallel review agents on cheap models cost less and covered about 75% of issues. Even Haiku scored better than the default Claude review. Several passes reached 90%+. Tested on a 3500 line diff. URL: https://news.ycombinator.com/item?id=50131114. Upvotes: not shown. Theme: cheap parallel models.

55. HN, `wren6991`, 2026-06-23. GLM-5.2 meets their needs for code and doc reviews. API rates are cheaper than Haiku. They ran it at home. URL: https://news.ycombinator.com/item?id=48651578. Upvotes: not shown. Theme: cheap model reviews.

56. X, Kevin Kern, 2025-12-28. Shows a self-hosted review setup on the pi coding agent: swap models, set your own reviewer prompts, stay within your subscription. URL: https://x.com/kevinkern/status/2005372506828603559. Likes: not visible. Theme: self-hosted, own prompts, BYO.

57. X, dan ushman, 2026-11-05. DeepSeek V4.1 Flash on 4x Sparks codes at 105 tok/s, nearly 2x what they get from Claude. The reviewer model is GLM 5.3 flash, also local, at 60 tok/s. URL: https://x.com/danushman/status/2118455005560660121. Likes: not visible. Theme: local DeepSeek.

58. X, benmaritz, 2026-08-31. Compared Chinese models at finding bugs in 9 real PRs they shipped. "deepseek wins." The models are still behind frontier labs for real coding work. Informal. URL: https://x.com/benmaritz/status/2094311261555359986. Likes: not visible. Theme: DeepSeek for review.

59. HN, `virgildotcodes`, 2026-04-20. "I really wish local models could compete with Codex, but they are miles apart for now." URL: https://news.ycombinator.com/item?id=47836435. Upvotes: not shown. Theme: local limit.

60. HN, `rbehrends`, 2026-11-06. Pushes back on "just run a local model": one production issue the frontier model would not make costs thousands. Also says the frontier is jagged, with errors in both directions. URL: https://news.ycombinator.com/item?id=50399505. Upvotes: not shown. Theme: local vs frontier.

61. HN, `upmostly`, 2026-06-25. Co-creator of Mira: self-hosted, BYOK, open source, point it at OpenRouter or a local model so nothing leaves your infra. About 77s per review. Vendor post. URL: https://news.ycombinator.com/item?id=48670525. Upvotes: not shown. Theme: competitor.

---

## G. Prices people find fair or unfair

62. X, Jorge Castillo, 2026-03-09. "Holy moly. The new Claude Code Review tool costs $25 per PR." URL: https://x.com/JorgeCastilloPr/status/2031125264172732794. Likes: not visible. Theme: unfair price.

63. X, Jean P.D. Meijer, 2026-03-09. "how is Claude Code Review is $15 to $25 per PR?!" One review costs about as much as an unlimited sub to most review apps. URL: https://x.com/initjean/status/2031143406336970755. Likes: not visible. Theme: unfair price.

64. X, Claude (official), 2026-03-09. "Reviews generally average $15-25, billed on token usage... may be more expensive than other solutions, like our open source GitHub Action." URL: https://x.com/claudeai/status/2031088175456903667. Likes: not visible. Theme: vendor price anchor.

65. X, Flavio Adamo, 2026-03-10. Codex Plus at $20 gets about 40 to 100 reviews a month. At $15-25 each that would be $600-2,500 a month on Claude. URL: https://x.com/flavioAd/status/2031312025830224254. Likes: not visible. Theme: unfair price.

66. X, Marty Kausas, 2026-05-20. Cursor moved Bugbot from $40 per seat per month to usage pricing at about $1 to $1.50 per run. Seat pricing breaks when an agent does variable work. URL: https://x.com/marty_kausas/status/2057140155215921525. Likes: not visible. Theme: pricing model shift.

67. HN, `rileymichael`, 2026-10-16. $15-25 per review is "absurd" and they think it is easier and cheaper to launch their own agent. Another comment of theirs says each push triggers a review, so $100+ per PR is easy and an eng may open a dozen a day. URLs: https://news.ycombinator.com/item?id=50123907 and https://news.ycombinator.com/item?id=50124269. Upvotes: not shown. Theme: unfair price, build it yourself.

68. HN, `dkhenry`, 2026-03-03. Wishes Graphite had improved Git hosting and review instead of "trying to charge me $40 a month for an AI reviewer". URL: https://news.ycombinator.com/item?id=47238512. Upvotes: not shown. Theme: unfair price, wish.

69. HN, `i_strelov`, 2026-01-13. After CodeRabbit, Greptile and others: "can't customize what it checks, pricing per seat, black-box prompts." Built diffray with readable Markdown agents. Vendor. URL: https://news.ycombinator.com/item?id=46600362. Upvotes: not shown. Theme: per-seat, black box.

70. HN, `nnorbert`, 2026-10-03. After trying CodeRabbit, Greptile, Copilot and Bugbot: too much noise, config issues, "unpredictable pricing" and platform lock-in. Built Vetoo for precision. Vendor. URL: https://news.ycombinator.com/item?id=49946872. Upvotes: not shown. Theme: noise, pricing, lock-in.

71. HN, `vjerancrnjak`, 2026-04-28. Angry that Copilot code review will use GitHub Actions minutes. Calls it turning compute into waste. URL: https://news.ycombinator.com/item?id=47938018. Upvotes: not shown. Theme: unfair hidden cost.

72. HN, `eranation`, 2026-06-05. CodeRabbit charging only $20 while Claude Code estimates the same cost for one PR makes the praise look like an ad. URL: https://news.ycombinator.com/item?id=48411433. Upvotes: not shown. Theme: fair price seen as suspicious.

---

## H. Compliance, audit, merge gates

73. HN, `ARayOutOfBounds`, 2026-07-23. See item 41: regulated code needs a reviewer that is not the author's model family. Theme: compliance.

74. HN, `DavidYoussef`, 2026-02-06. See item 46: auto-approve trivial changes with an evidence log, human time for the rest. Theme: audit evidence.

75. HN, `paimapi`, 2026-09-16, on the post "ImpactGate: A merge gate that scores structural decay AI adds". Expects a future of automated quality checks on AI-drafted code, with humans checking that business logic and acceptance criteria hold. Says existing tools do not measure quality well. URL: https://news.ycombinator.com/item?id=49727585. Upvotes: not shown. Theme: merge gate.

76. HN, `bigfishrunning` and `divbzero`, 2025-12-22 and 2025-12-23, quoting a post: "The pull request model on GitHub doesn't carry enough information to review AI generated code properly. I wish I could see the prompts that led to changes." Another user replies "Yes! Who is building this?" URLs: https://news.ycombinator.com/item?id=46354197 , https://news.ycombinator.com/item?id=46361790 , https://news.ycombinator.com/item?id=46355274. Upvotes: not shown. Theme: audit trail, wish.

77. HN, `laurencoral`, 2026-07-08. Built a menu-bar app that records what coding agents do so you can read it after the session. Cites approval fatigue. URL: https://news.ycombinator.com/item?id=48830674. Upvotes: not shown. Theme: audit log of agent actions.

78. X search summary, 2026. A CI-triage example keeps merge approval with the human, lets the agent open only a draft PR, and only after verification passes. Author not shown in the summary. Not verified.

I found no real post that links AI code review to SOC 2 or similar controls. Nothing to cite on named compliance frameworks. Not verified.

---

## I. OSS slop

79. X, Mitchell Hashimoto (via a retweet by Dillon Mulroy), 2026-01-22. "Ghostty is getting an updated AI policy. AI assisted PRs are now only allowed for accepted issues. Drive-by AI PRs will be closed without question. Bad AI drivers will be banned." URL: https://x.com/dillon_mulroy/status/2014433070154162382. Likes: not visible. Theme: OSS slop policy.

80. X, JFrog Security, 2026-01-27. curl ends its bug bounty after almost 7 years, citing a flood of AI "slop" reports. URL: https://x.com/JFrogSecurity/status/2016074580390019468. Likes: not visible. Theme: OSS slop.

81. X, Katie Paxton-Fear, 2026-01-20. On curl: "Stop using AI to 'find bugs' when you don't know what you're doing. LEARN INSTEAD." URL: https://x.com/InsiderPhD/status/2013539187257983414. Likes: not visible. Theme: OSS slop.

82. X, Peak (Coolify maintainer), 2026-02-14. Built "Anti Slop", a GitHub Action that finds and auto-closes low-quality AI PRs. Early tests: it could have closed 98% of slop PRs. URL: https://x.com/peaklabs_dev/status/2022787697073381852. Likes: not visible. Theme: OSS slop, gate tooling.

83. X, RPCS3, 2026-05-11. Posted guidelines for AI-generated code. Will block those who submit it without disclosing. URL: https://x.com/rpcs3/status/2053632969357902050. Likes: not visible. Theme: OSS slop.

84. X, PC Gamer, 2026-06-30. Godot will no longer accept AI-authored code contributions. URL: https://x.com/pcgamer/status/2072012544416665977. Likes: not visible. Theme: OSS slop.

85. X, libukai, 2026-01-16. tldraw said it will stop accepting external PRs. The poster adds that maintainers can just use AI themselves, so outside PRs add little. URL: https://x.com/libukai/status/2012064287884525961. Likes: not visible. Theme: OSS closes the gate.

86. HN, `acemarke` (Redux maintainer), 2026-11-04. The default behavior of agents is to "pile slop on slop", make poor architecture choices and patch edge cases. This is worse without oversight and with weaker models. URL: https://news.ycombinator.com/item?id=50368991. Upvotes: not shown. Theme: slop.

87. HN, `projectazorian`, 2025-10-15, in a thread titled "I am a programmer, not a rubber-stamp that approves Copilot generated code". Quotes another user: LLM code "looks right at first glance, and passes superficial tests, so it's easy to get merged", then needs a rewrite later. URL: https://news.ycombinator.com/item?id=45595687. Upvotes: not shown. Theme: review burden pushed to others.

---

## J. Explicit feature wishes (verbatim or near verbatim)

W1. "I wish I could see the prompts that led to changes." (HN, 2025-12-22, https://news.ycombinator.com/item?id=46354197)

W2. Wants "verify previous comments only" instead of new comments on every push. A GitHub Community discussion about Copilot review raised this (found in search, not opened). URL: https://github.com/orgs/community/discussions/189767. Not verified beyond the snippet.

W3. Wants to customize what the reviewer checks and read its prompt (i_strelov, item 69).

W4. Wants "only findings we would actually act on" (`oscarestudillom`, 2026-09-22, https://news.ycombinator.com/item?id=49798887).

W5. Wants a review that can use the same agents before push and in CI, so a false positive can be checked with full context (`CharlieDigital`, 2026-09-16, https://news.ycombinator.com/item?id=49729438).

W6. Wants a reviewer that uses a different vendor from the author (items 42, 43).

W7. Wants a gate that is "mechanically enforced, not based on instructions in skills" (item 44).

W8. Wants the reviewer to compare code to the ticket (PJDK, item 51).

W9. Wants auto-approve of trivial changes with an evidence log (item 46).

W10. Wants predictable cost: usage caps or flat price, not $15-25 per PR (items 62 to 70).

W11. Wants nothing to leave their infra, BYO key, swap models (items 49, 56, 61).

W12. Wants to review the decision trace or plan, not only the diff (`BonanKou`, 2026-08-18, https://news.ycombinator.com/item?id=49340321).

W13. Wants "a real first class alternative" to GitHub for hosting and review, not an AI add-on (item 68).

---

## Top 10 explicit feature wishes, ranked by frequency

Counts are how many of the items above carry the wish. HN and X only. Rough, not a survey.

1. **Fewer, higher-precision findings.** Only comments the author would act on. About 12 items (1 to 6, 12, 16, 17, 70, W4).
2. **Predictable, cheaper pricing.** No $15-25 per PR, no surprise per-seat or Actions-minute costs. About 12 items (62 to 72, 50).
3. **Self-hosted, BYO key, swap models, nothing leaves our infra.** About 10 items (49 to 58, 61, 69).
4. **A reviewer that is independent of the author model and context.** About 7 items (41 to 44, 42, W6, 47).
5. **Proof the tests can fail.** Tests first from the spec, red before code, no deleted or faked tests. About 8 items (31 to 33, 36, 37, 38, 39, 45).
6. **Customizable, readable review rules and team memory.** Edit the prompt, add cross-PR memory. About 5 items (50, 69, W3, 47, 52).
7. **Show prompts, plan or decision trace with the PR.** About 5 items (76, W1, W12, 29, 77).
8. **Risk triage with an evidence log.** Auto-approve trivial changes, spend human time on the rest. About 4 items (46, 22, 26, 74).
9. **Reviews that converge.** No new findings on every push, no 50-loop author versus reviewer. About 4 items (13, 29, W2, 24).
10. **Mechanical enforcement over prompts, with a human on the exact change.** Gate in code, human approves, agent cannot approve itself. About 5 items (44, 32, 27, 29, 78).

Runners-up: compare the PR to the ticket (item 51), keep PRs small (item 25), review the same checks locally before push (W5).

---

## 5 surprises that contradict the obvious story

1. **AI review is not seen as useless.** One engineer calls it the single most valuable AI dev feature (item 7). Another says BugBot is "great" and rarely wrong on medium or high findings (item 9). The complaint is about specific tools and about volume, not about the category.

2. **Cheap models are good enough for review, and smarter models are not clearly better.** A DeepSeek-flash precision gate plus Qwen matched CodeRabbit's F1 for about $7 on 50 PRs (item 53). Three Haiku-class agents beat the default Claude review (item 54). Smarter models had slightly worse recall in one test (item 14). This supports Heimdall's DeepSeek bet.

3. **The price anger is about per-PR surprise, not the sticker.** CodeRabbit at $20 drew suspicion, not praise (item 72). The loud outrage was at $15-25 per Claude review and at per-push billing (items 62 to 67). Cursor itself moved from $40 per seat to usage pricing (item 66). "Predictable" matters more than "cheap".

4. **More review rounds do not make it safer.** A fresh review keeps finding about 8 new things (item 13), and an author agent and reviewer agent looped 50+ times overnight (item 29). People also say same-vendor reviewers share blind spots (items 41 to 43). So volume of AI review is a weak signal. A hard stop (a proof, a gate, a human on the exact commit) beats another round.

5. **The real competitor is a half-day DIY script, and the real fear is the human gate wearing out.** Several teams built their own reviewer on a self-hosted runner or one VM in hours (items 49, 51, 52, 67). Meanwhile people name approval fatigue and rubber-stamping as the failure (items 24, 27, 87), and OSS projects respond by closing the gate to outsiders rather than adding bots (items 79 to 85). Local models are seen as too weak (items 59, 60), so BYO API key is more common than true local inference.

---

## Not verified list

- All Reddit content (blocked). Only two second-hand mentions in section A.
- Like, view and upvote counts for every item.
- X posts were read from search result titles only. The body of long posts and articles was not opened. Items 17, 19, 39, 40, 48, 78 are snippet-level.
- Item 15 (sensanaty) and item 29 (Claude Code team write-up) were read in part or through summaries.
- Vendor posts are marked "Vendor". Treat their numbers as claims.
- GitHub discussion 189767 (W2) was seen in a search snippet only.
- No post found that links AI code review to SOC 2 or a named compliance framework.
