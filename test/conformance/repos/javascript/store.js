export class Store {
  constructor() {
    this.values = {};
  }

  put(key, value) {
    this.values[key] = value;
  }
}

export function newStore() {
  return new Store();
}
