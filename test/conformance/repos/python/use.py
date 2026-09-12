from store import new_store


def use_store() -> None:
    s = new_store()
    s.put("a", "1")
    s.put("b", "2")
