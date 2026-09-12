import { newStore } from "./store";

export function useStore(): void {
  const s = newStore();
  s.put("a", "1");
  s.put("b", "2");
}
