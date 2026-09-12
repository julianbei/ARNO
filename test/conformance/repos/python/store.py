class Store:
    def __init__(self) -> None:
        self.values: dict[str, str] = {}

    def put(self, key: str, value: str) -> None:
        self.values[key] = value


def new_store() -> Store:
    return Store()
