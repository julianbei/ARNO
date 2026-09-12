class Store
  def initialize
    @values = {}
  end

  def put(key, value)
    @values[key] = value
  end
end

def new_store
  Store.new
end
