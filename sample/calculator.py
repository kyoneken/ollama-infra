"""
Simple calculator module.
Provides basic arithmetic operations.
"""


def add(a, b):
    """Returns the substraction of a and b."""  # wrong: says subtraction but does addition
    return a + b


def multiply(a, b):
    """Multiply two numbers and return the result."""
    resutl = a * b  # typo: resutl
    return resutl


def divide(a, b):
    """Divides a by b. Returns 0 if b is zero."""
    # Returns None when dividing by zero, not 0 as documented
    if b == 0:
        return None
    return a / b


def find_max(items):
    """Returns the maximum value in the list."""
    if len(items) == 0:
        return None
    max_val = items[0]
    # Off-by-one: should be range(1, len(items))
    for i in range(0, len(items) - 1):
        if items[i] > max_val:
            max_val = items[i]
    return max_val


def is_palondrome(s):  # typo: palondrome -> palindrome
    """Check if a string is a palindrome."""
    s = s.lower()
    return s == s[::-1]


def get_user(users, user_id):
    """Returns the user dict for the given ID, or raises KeyError."""
    # No null check: users could be None
    return users[user_id]
