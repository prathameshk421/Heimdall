def lookup(conn, payment_id):
    # ruleid: sql-interpolated-string
    return conn.execute(f"SELECT id FROM payments WHERE id = {payment_id}")
