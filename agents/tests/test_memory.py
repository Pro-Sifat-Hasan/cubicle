import asyncio

from cubicle_agents.llm.base import Turn
from cubicle_agents.staff import memory
from cubicle_agents.staff.roster import DEFAULT_ROSTER
from cubicle_agents.tools import build_toolbox

from fakes import FakeGateway


def run(coro):
    return asyncio.run(coro)


FACTS = [  # newest first
    "Sam lives in Lisbon and works in UTC+0.",
    "Sam's main client is Acme Ltd, a logistics company.",
    "Sam prefers short emails with no greeting.",
    "The Acme contract renews in March.",
    "Sam's dog is called Biscuit.",
    "Sam uses Python and FastAPI for backend work.",
    "Sam's sister Maya is a doctor.",
    "Sam's budget for ads is 500 euros a month.",
    "Sam dislikes meetings before 10am.",
    "Sam is vegetarian.",
    "Sam banks with Monzo.",
    "Sam's favourite editor is VS Code.",
    "Sam coaches a youth football team on Saturdays.",
    "Sam's laptop is a ThinkPad.",
    "Sam reads the news at 7am.",
]


def test_short_memory_is_sent_whole():
    assert memory.select_memories(FACTS[:5], "anything") == FACTS[:5]


def test_relevant_facts_are_chosen_for_the_request():
    chosen = memory.select_memories(FACTS, "Draft an email to Acme about the contract", relevant=2, recent=1)
    assert "The Acme contract renews in March." in chosen
    assert "Sam's main client is Acme Ltd, a logistics company." in chosen
    assert FACTS[0] in chosen  # the newest fact is always in
    assert "Sam's dog is called Biscuit." not in chosen
    assert chosen == [f for f in FACTS if f in chosen]  # newest-first order is kept


def test_memory_budget_is_respected():
    big = [f"Fact number {i}: " + "word " * 80 for i in range(40)]
    assert sum(len(f) for f in memory.select_memories(big, "word fact", budget_chars=1000)) <= 1000


def test_secrets_are_recognised():
    # The fake secrets are assembled here so that no real-looking key sits in the source for scanners to flag.
    fakes = [
        "Her API key is " + "sk-" + "abcdefghijklmnop1234",
        "password" + ": hunter2hunter2",
        "tok" + "en = " + "ghp_" + "abcdefghijklmnopqrstu12345",
        "card " + " ".join(["4111"] * 4),
        "-----BEGIN RSA PRIVATE" + " KEY-----",
        "AK" + "IA" + "ABCDEFGHIJKLMNOP",
    ]
    for text in fakes:
        assert memory.looks_secret(text), text
    for text in ["Sam prefers short emails", "The Acme contract renews in March", "Sam works at 9:30 daily"]:
        assert not memory.looks_secret(text), text


def test_old_long_turns_are_cut_but_the_newest_stay_whole():
    long = "x" * 3000
    history = [Turn("owner", long), Turn("agent", long), Turn("owner", "short"), Turn("agent", long), Turn("owner", long)]
    out = memory.trim_history(history)
    assert [t.role for t in out] == ["owner", "agent", "owner", "agent", "owner"]
    assert len(out[-1].content) == 3000 and len(out[-2].content) == 3000  # the two newest are whole
    assert len(out[0].content) < 800 and out[0].content.endswith("[…]")


def test_a_long_chat_keeps_only_the_latest_turns():
    history = [Turn("owner" if i % 2 == 0 else "agent", f"message {i}") for i in range(60)]
    out = memory.trim_history(history)
    assert len(out) == memory.HISTORY_TURNS and out[-1].content == "message 59"


def _toolbox(gw):
    return build_toolbox(employee=DEFAULT_ROSTER[1], workspace=__import__("pathlib").Path("."), gateway=gw,
                         may_ask=False, client_web_fetch=False)


def test_remember_refuses_secrets_and_forget_removes_a_fact():
    gw = FakeGateway(memories=["Sam is vegetarian."])
    box = _toolbox(gw)
    refused = run(box.call("remember", {"fact": "The API key is sk-abcdefghijklmnop1234"}))
    assert refused.is_error and gw.memories == ["Sam is vegetarian."]
    assert not run(box.call("remember", {"fact": "Sam prefers tea."})).is_error
    assert run(box.call("forget", {"fact": "Sam is vegetarian."})).content.startswith("Removed")
    assert "Sam is vegetarian." not in gw.memories
    assert run(box.call("forget", {"fact": "something never saved"})).is_error


def test_a_fact_that_repeats_a_stored_one_is_not_saved_again():
    gw = FakeGateway(memories=["Sam prefers short emails."])
    box = _toolbox(gw)
    assert "Already" in run(box.call("remember", {"fact": "Sam prefers short emails"})).content
    assert gw.memories == ["Sam prefers short emails."]
    assert run(box.call("remember", {"fact": "x" * 400})).is_error  # a fact is one short sentence
    assert not run(box.call("remember", {"fact": "Sam lives in London."})).is_error
    assert memory.is_duplicate("sam prefers SHORT emails", gw.memories)
    assert not memory.is_duplicate("Sam likes tea", gw.memories)


def test_the_profile_is_replaced_whole_and_refuses_secrets():
    gw = FakeGateway()
    box = _toolbox(gw)
    assert not run(box.call("update_profile", {"profile": "Sam is an AI engineer."})).is_error
    assert not run(box.call("update_profile", {"profile": "Sam is an AI engineer in London."})).is_error
    assert gw.profile == "Sam is an AI engineer in London."
    fake_secret = "tok" + "en = " + "abcdef" + "123456"  # built at run time so secret scanners do not flag the test
    assert run(box.call("update_profile", {"profile": fake_secret})).is_error
    assert run(box.call("update_profile", {"profile": "y" * 2000})).is_error


def test_compact_memory_lists_then_merges_facts():
    gw = FakeGateway(memories=["Sam works as a developer.", "Sam works with Python.", "Sam likes tea."])
    box = _toolbox(gw)
    listing = run(box.call("compact_memory", {})).content
    assert "Sam works with Python." in listing and "Sam likes tea." in listing
    done = run(box.call("compact_memory", {"remove": ["Sam works as a developer.", "Sam works with Python."],
                                           "replace_with": "Sam is a Python developer."}))
    assert "Removed 2" in done.content
    assert sorted(gw.memories) == ["Sam is a Python developer.", "Sam likes tea."]


def test_the_profile_and_a_long_memory_warning_reach_the_prompt(tmp_path):
    from cubicle_agents.staff import prompts
    system = prompts.system_prompt(DEFAULT_ROSTER[1], "Sam", memories=["Sam likes tea."], tool_mode=True, web=False,
                                   profile="Sam is an AI engineer.", memory_count=prompts.COMPACT_AT)
    assert "Sam is an AI engineer." in system and "compact_memory" in system
    short = prompts.system_prompt(DEFAULT_ROSTER[1], "Sam", memories=[], tool_mode=True, web=False, memory_count=5)
    assert "too many" not in short


def test_text_mode_profile_directive_replaces_the_profile(tmp_path):
    from fakes import ScriptedLLM, claim, make_handler
    gw = FakeGateway(memories=["Sam signs emails as S."])
    llm = ScriptedLLM("PROFILE: Sam is an AI engineer.\nREMEMBER: Sam signs emails as S\n"
                      "REMEMBER: Sam hates meetings.\nBody")
    writer = next(e for e in DEFAULT_ROSTER if e.id == "writer")
    run(make_handler(gw, llm, tmp_path).handle(writer, claim()))
    assert gw.profile == "Sam is an AI engineer."
    assert gw.memories == ["Sam signs emails as S.", "Sam hates meetings."]  # the repeat is skipped
    assert gw.completed["tsk_1"] == "Body"
