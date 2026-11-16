"""POST /refunds (KAN-23). The service keeps the refunds it accepted per payment."""


class RefundService:
    def __init__(self, payments):
        self.payments = payments  # payment_id -> {"merchant_id": str, "captured": int}
        self.refunded = {}
        self.next_id = 0

    def refund(self, payment_id, amount):
        """Return (status, body). body is a dict for 201 and None otherwise."""
        payment = self.payments.get(payment_id)
        if payment is None:
            return 404, None
        already = self.refunded.get(payment_id, 0)
        if amount > payment["captured"]:
            return 400, None
        self.refunded[payment_id] = already + amount
        self.next_id += 1
        body = {
            "id": "rf_%d" % self.next_id,
            "payment_id": payment_id,
            "amount": amount,
            "merchant_id": payment["merchant_id"],
        }
        return 201, body
