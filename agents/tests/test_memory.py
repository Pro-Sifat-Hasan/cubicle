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
    for text in ["Her API key is sk-abcdefghijklmnop1234", "password: hunter2hunter2",
                 "token = ghp_abcdefghijklmnopqrstu12345", "card 4111 1111 1111 1111",
                 "-----BEGIN RSA PRIVATE KEY-----", "AKIAABCDEFGHIJKLMNOP"]:
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
