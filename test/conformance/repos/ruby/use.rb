require_relative "store"

def use_store
  s = new_store
  s.put("a", "1")
  s.put("b", "2")
  s
end
