import fs from "node:fs";
import path from "node:path";

type User = {
  id: string;
};

export class SessionStore {
  open() {
    return true;
  }
}

export function makeUser(id: string): User {
  return { id };
}
