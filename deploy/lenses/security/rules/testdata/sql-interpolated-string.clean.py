def lookup(conn, payment_id):
    return conn.execute("SELECT id FROM payments WHERE id = %s", (payment_id,))
