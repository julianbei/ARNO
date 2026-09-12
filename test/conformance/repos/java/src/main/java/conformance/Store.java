package conformance;

import java.util.HashMap;
import java.util.Map;

public class Store {
    private final Map<String, String> values = new HashMap<>();

    public void put(String key, String value) {
        values.put(key, value);
    }
}
