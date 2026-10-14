"""Illustrative generated Python recognizer for a deliberately small subset.

Source grammar represented by this example: rule main = ("a" / "b")*
The recognizer accepts only strings consisting entirely of ASCII a and b.
This example is not wired to PEGO or a conformance test.
"""


class ParseError(Exception):
    def __init__(self, position, expected):
        self.position = position
        self.expected = tuple(sorted(set(expected)))
        super().__init__(
            f"parse failed at {position}; expected {', '.join(self.expected)}"
        )


class Parser:
    def __init__(self, text):
        self.text = text
        self.farthest = -1
        self.expected = set()

    def fail(self, position, expected):
        if position > self.farthest:
            self.farthest = position
            self.expected = {expected}
        elif position == self.farthest:
            self.expected.add(expected)

    def literal(self, position, value):
        if self.text.startswith(value, position):
            return position + len(value)
        self.fail(position, repr(value))
        return None

    def rule_main(self, position):
        # Each ordered-choice alternative begins at the same input position.
        # Repetition stops on failure and verifies that each success advances.
        while True:
            item_start = position
            next_position = self.literal(item_start, "a")
            if next_position is None:
                next_position = self.literal(item_start, "b")
            if next_position is None:
                return position
            if next_position == item_start:
                raise RuntimeError("generated repetition made no progress")
            position = next_position

    def recognize(self):
        end = self.rule_main(0)
        if end == len(self.text):
            return
        self.fail(end, "end of input")
        raise ParseError(self.farthest, self.expected)


def recognize(text):
    Parser(text).recognize()
