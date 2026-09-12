export class Store {
  private values: Record<string, string> = {};

  put(key: string, value: string): void {
    this.values[key] = value;
  }
}

export function newStore(): Store {
  return new Store();
}
