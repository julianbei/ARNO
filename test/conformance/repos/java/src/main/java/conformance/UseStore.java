package conformance;

public class UseStore {
    public static Store build() {
        Store s = new Store();
        s.put("a", "1");
        s.put("b", "2");
        return s;
    }
}
