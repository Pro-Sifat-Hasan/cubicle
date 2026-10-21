package catalog

// More built-in skills, attached to the agents they help (see agents.go). They are instructions in the
// SKILL.md style: an agent reads the description, and loads the full text when a task matches.
var moreSkills = []SkillTemplate{
	{
		ID: "debugging", Name: "debugging",
		Description: "Use when something is broken: reproduce, isolate, find the cause, fix it and prove the fix.",
		Content: `# Debugging

1. **Reproduce**: get the smallest input or steps that fail every time. If you cannot reproduce it, say so and list what you tried.
2. **Read the evidence first**: the full error message, stack trace and logs. Do not guess before you have read them.
3. **Isolate**: narrow down with find_files and grep_files; check the last change that touched the area.
4. **Hypothesise**: state one cause at a time and a check that would prove or disprove it. Run the check.
5. **Fix the cause, not the symptom**: smallest change that removes the cause. Do not hide errors with a catch-all.
6. **Prove it**: add or run a test that failed before and passes now, and run the related tests. Report the real output.
7. **Report**: cause, fix, how you verified it, and anything similar that may break elsewhere.`,
	},
	{
		ID: "test-writing", Name: "test-writing",
		Description: "Use when writing or improving automated tests: what to cover, how to structure them, and running them.",
		Content: `# Writing tests

- Test behaviour, not implementation: inputs and outputs, not private details.
- One idea per test, named for the behaviour ("rejects an empty name").
- Cover: the normal case, boundaries (empty, one, many, maximum), invalid input, and each error path.
- Arrange, act, assert. No logic or loops in assertions; no tests that depend on order or the clock.
- Use the project's existing test framework, folder and naming; match its style.
- Fake slow or external things (network, time, disk) but not the code under test.
- Run the tests (request approval for the command) and report what really happened. A new test should fail if the behaviour breaks: check that.`,
	},
	{
		ID: "git-workflow", Name: "git-workflow",
		Description: "Use for version control work: small commits, clear messages, branches, and safe habits.",
		Content: `# Git workflow

- Check state first: git status and git diff. Commit only what belongs together.
- Small, self-contained commits. Message: a short imperative summary (under 72 characters), then why, not just what.
- Follow the project's convention (for example Conventional Commits: feat(scope): ..., fix(scope): ...).
- Work on a branch, not on main. Name it for the change (feature/short-name, fix/short-name).
- Never rewrite shared history, never force-push, never commit secrets, tokens, .env files or databases.
- Pushing, pulling and cloning are not available to you: say what the owner should run.`,
	},
	{
		ID: "refactoring", Name: "refactoring",
		Description: "Use when cleaning up code without changing what it does: small safe steps backed by tests.",
		Content: `# Refactoring

1. Make sure there are tests for the behaviour. If not, write them first and run them.
2. Change one thing at a time: rename, extract a function, remove duplication, simplify a condition.
3. Run the tests after every step. If one fails, undo that step.
4. Keep behaviour identical. A bug you find gets its own fix and its own note, not a quiet change.
5. Match the surrounding style and naming. Do not reformat unrelated code.
6. Stop when the code is clearer; do not redesign.`,
	},
	{
		ID: "api-design", Name: "api-design",
		Description: "Use when designing or changing an API: resources, errors, versioning and a clear contract.",
		Content: `# API design

- Name resources as nouns, actions as HTTP methods; keep names and casing consistent.
- Define every request and response: fields, types, required or optional, limits.
- Errors: a stable shape with a machine-readable code and a human message; correct status codes.
- Validate all input at the edge; never trust client data.
- Be explicit about pagination, filtering, idempotency and rate limits.
- Changing an existing API: list what breaks for current callers, and prefer additive changes.
- Write the contract down (docs and examples) before the code, and keep them in sync.`,
	},
	{
		ID: "security-review", Name: "security-review",
		Description: "Use for a defensive security review of code or configuration: common weaknesses, read-only by default.",
		Content: `# Security review (defensive)

Check, and report findings by severity with the file and line:
- **Input**: injection (SQL, command, path traversal), unvalidated or oversized input, unsafe deserialisation.
- **Secrets**: keys, tokens or passwords in code, config, logs or history.
- **Access**: missing authentication or authorisation, over-broad permissions, default credentials.
- **Data**: sensitive data stored or sent in clear text, verbose errors that leak internals.
- **Dependencies**: known-vulnerable or unmaintained packages; unpinned versions.
- **Output**: untrusted text rendered without escaping.

Stay read-only: describe the fix, do not exploit anything or touch live systems. State what you could not check.`,
	},
	{
		ID: "technical-writing", Name: "technical-writing",
		Description: "Use when writing documentation: READMEs, how-to guides and API docs people actually read.",
		Content: `# Technical writing

- Start with what it is and who it is for in one or two sentences.
- Show the fastest path first: install and run a working example in under five steps.
- Then explain concepts, options and reference material, in that order.
- Every command and code sample must be copy-pasteable and correct; say what the output should look like.
- Short sentences, active voice, consistent terms. Use headings and lists for scanning.
- Document only what you verified from the code or the owner. List open questions instead of guessing.`,
	},
	{
		ID: "data-analysis", Name: "data-analysis",
		Description: "Use when analysing data or a spreadsheet: check quality, compute carefully and explain the finding.",
		Content: `# Data analysis

1. **Understand the question** and what decision it supports.
2. **Look at the data**: columns, types, row count, missing values, duplicates, outliers. Report problems before analysing.
3. **Compute** step by step; show the method and the numbers. Double-check totals and units.
4. **Compare fairly**: same period, same population; note sample sizes.
5. **Explain**: a headline finding, the key numbers in a small table, what it means, and how sure you are.
6. Never extrapolate beyond the data or present a correlation as a cause.`,
	},
	{
		ID: "sql-help", Name: "sql-help",
		Description: "Use when writing or reviewing SQL: correct joins, safe queries and readable results.",
		Content: `# SQL

- Ask for or look up the table definitions; do not guess column names.
- Be explicit about joins and what happens to unmatched rows. Check for duplicate rows after a join.
- Filter before you aggregate; make NULL handling deliberate.
- Read-only by default: SELECT first. Any UPDATE or DELETE needs a WHERE clause, a count of affected rows checked first, and the owner's approval.
- Format for reading: one clause per line, meaningful aliases, comments for non-obvious logic.
- Explain what the query returns in a sentence.`,
	},
	{
		ID: "copywriting", Name: "copywriting",
		Description: "Use when writing marketing copy: headlines, landing pages, ads and emails that convince honestly.",
		Content: `# Copywriting

1. Know the reader, their problem and the one action you want.
2. Headline: the clearest benefit, in plain words. Offer 5 options in different styles.
3. Body: problem, then solution, then proof (facts, numbers, quotes the owner gave you), then a single call to action.
4. Specific beats vague: replace "fast" with how fast. Never invent statistics, testimonials or guarantees.
5. Short sentences, concrete verbs, no jargon. Read it aloud.
6. Give two variants and say which you would test first and why.`,
	},
	{
		ID: "seo-basics", Name: "seo-basics",
		Description: "Use for search optimisation of a page or article: keywords, structure and metadata.",
		Content: `# SEO basics

- Pick one primary search intent per page and a few related terms; do not stuff keywords.
- Title (under about 60 characters) and meta description (under about 155) that say what the page offers.
- One H1, logical H2/H3 headings, short paragraphs, descriptive link text and image alt text.
- Answer the question early; add useful depth, examples and internal links.
- Check facts and cite sources. Say what you could not measure: you cannot see rankings or traffic without data from the owner.`,
	},
	{
		ID: "project-planning", Name: "project-planning",
		Description: "Use to plan a project or a week: goals, breakdown, estimates, dependencies and risks.",
		Content: `# Project planning

1. **Goal and done-criteria**: what must be true when it is finished.
2. **Breakdown**: tasks small enough to finish in a day or less, each with an owner.
3. **Order and dependencies**: what blocks what; what can run in parallel.
4. **Estimates**: a range, not one number; state assumptions.
5. **Risks**: the top three, each with a mitigation or a trigger to re-plan.
6. **Next step**: the first concrete action and when.

Present it as a table or checklist with dates. Flag conflicts early.`,
	},
	{
		ID: "user-stories", Name: "user-stories",
		Description: "Use when writing requirements: user stories with acceptance criteria and priorities.",
		Content: `# User stories

Format: As a [user], I want [capability], so that [benefit].

For each story:
- **Acceptance criteria**: testable "Given / When / Then" lines, including an error case.
- **Out of scope**: what it does not include.
- **Priority**: must, should or could, with the reason (impact versus effort).
- **Open questions** for the owner.

Keep stories small and independent. Do not invent user research; mark assumptions.`,
	},
	{
		ID: "study-plan", Name: "study-plan",
		Description: "Use to teach or plan learning: explain from first principles, practise, and check understanding.",
		Content: `# Teaching and study plans

- Find out what the learner already knows and what they need it for.
- Explain from first principles with one concrete example before any jargon.
- Check understanding with two or three short questions; adjust.
- Plan: weekly goals, short daily sessions, spaced review of old material, a small project to apply it.
- Give practice questions with answers separated, so they can try first.`,
	},
	{
		ID: "itinerary-planning", Name: "itinerary-planning",
		Description: "Use to plan a trip: realistic days, travel times, costs and what to book.",
		Content: `# Trip planning

- Ask or assume: dates, budget, pace, interests, who is travelling. State assumptions.
- Day by day: morning, afternoon, evening; group nearby places; include travel times and opening hours.
- Check current information on the web (hours, prices, entry requirements, weather) and cite it.
- End with a booking list: what to reserve, by when, and rough costs.
- Note visa, health and safety points to verify with official sources.`,
	},
	{
		ID: "budget-planning", Name: "budget-planning",
		Description: "Use for budgets and money comparisons: clear categories, arithmetic shown, general information only.",
		Content: `# Budgets

- List income and fixed costs first, then variable costs, then savings goals.
- Show every calculation; state currency, period and assumptions.
- Compare options on total cost over the same period, including fees.
- Give general information, not regulated financial advice; say when a professional should be consulted.
- Mark any figure you were not given as an estimate.`,
	},
	{
		ID: "customer-reply", Name: "customer-reply",
		Description: "Use when answering a customer: acknowledge, solve, and set expectations in a short, kind reply.",
		Content: `# Customer replies

1. Acknowledge the problem in one sentence, in their words.
2. Give the answer or the next step first; keep it short.
3. Use only policies and facts the owner has given you. If unsure, say what you will check and when you will reply.
4. Never promise refunds, dates or features that were not confirmed.
5. End with one clear next step. Warm, plain tone; no blame.`,
	},
	{
		ID: "translation-check", Name: "translation-check",
		Description: "Use when translating: keep meaning and tone, adapt idioms, flag ambiguity.",
		Content: `# Translation

- Translate meaning and tone, not word for word. Adapt idioms and units; keep names, numbers and formatting.
- Flag ambiguous phrases and offer the alternative readings.
- Keep a consistent glossary for repeated terms.
- Re-read the result as a native reader would, then back-check a few key sentences against the source.`,
	},
	{
		ID: "fact-checking", Name: "fact-checking",
		Description: "Use to verify claims: find the best source, rate each claim, and cite it.",
		Content: `# Fact checking

For each claim:
1. Find the best primary or official source and open it.
2. Rate it: **confirmed**, **partly true**, **disputed**, **unverified** or **false**.
3. Quote the relevant line and give the URL and its date.
4. Flag outdated figures, missing context and misleading framing.

Never rate something confirmed from memory alone.`,
	},
	{
		ID: "proofreading", Name: "proofreading",
		Description: "Use to polish text: fix errors, tighten wording, keep the author's voice, and list what changed.",
		Content: `# Proofreading

1. Fix spelling, grammar, punctuation and consistency (names, dates, numbers, tense).
2. Tighten: cut filler and repetition; split long sentences.
3. Keep the author's voice and meaning; do not add new claims.
4. Return the improved text, then a short list of the changes that matter and anything the author must decide.`,
	},
}
