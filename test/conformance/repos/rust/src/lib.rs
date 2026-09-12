use std::collections::HashMap;

pub struct Store {
    values: HashMap<String, String>,
}

impl Store {
    pub fn new() -> Store {
        Store { values: HashMap::new() }
    }

    pub fn put(&mut self, key: &str, value: &str) {
        self.values.insert(key.to_string(), value.to_string());
    }
}

pub fn use_store() -> Store {
    let mut s = Store::new();
    s.put("a", "1");
    s.put("b", "2");
    s
}
