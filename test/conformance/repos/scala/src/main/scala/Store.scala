class Store {
  private var values: Map[String, String] = Map.empty

  def put(key: String, value: String): Unit = {
    values = values + (key -> value)
  }
}

object UseStore {
  def build(): Store = {
    val s = new Store
    s.put("a", "1")
    s.put("b", "2")
    s
  }
}
