#!/usr/bin/env python3
"""A simple 70-second task for testing tb run."""

import time


def main() -> None:
    print("70-second test started", flush=True)
    time.sleep(70)
    print("70-second test completed", flush=True)


if __name__ == "__main__":
    main()
