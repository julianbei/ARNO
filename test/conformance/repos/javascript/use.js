import { newStore } from "./store.js";

export function useStore() {
  const s = newStore();
  s.put("a", "1");
  s.put("b", "2");
}
