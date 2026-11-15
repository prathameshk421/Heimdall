import sqlite3

class AccountService:
    def __init__(self, db_path=":memory:"):
        self.db = sqlite3.connect(db_path)
        self._init_db()

    def _init_db(self):
        with self.db:
            self.db.execute("CREATE TABLE IF NOT EXISTS accounts (id TEXT PRIMARY KEY, balance INTEGER NOT NULL)")

    def create_account(self, acc_id: str, initial_balance: int = 0):
        with self.db:
            self.db.execute("INSERT INTO accounts (id, balance) VALUES (?, ?)", (acc_id, initial_balance))

    def get_balance(self, acc_id: str) -> int:
        cur = self.db.cursor()
        cur.execute("SELECT balance FROM accounts WHERE id = ?", (acc_id,))
        row = cur.fetchone()
        if not row:
            raise KeyError(f"Account {acc_id} not found")
        return row[0]

    def transfer(self, from_id: str, to_id: str, amount: int):
        if amount <= 0:
            raise ValueError("Transfer amount must be positive")
        current = self.get_balance(from_id)
        if current < amount:
            raise ValueError("Insufficient funds")
        with self.db:
            self.db.execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", (amount, from_id))
            self.db.execute("UPDATE accounts SET balance = balance + ? WHERE id = ?", (amount, to_id))
