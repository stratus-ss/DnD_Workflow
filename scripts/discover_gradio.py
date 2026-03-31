"""
One-off script to discover the Gradio API endpoints on the e2a server.
Run once during development to map endpoint names and parameter shapes.

Usage:
    pip install gradio_client
    python discover_gradio.py
"""

from gradio_client import Client

E2A_URL = "https://e2a.example.com"


def main():
    client = Client(E2A_URL, ssl_verify=False)
    print("=== API Endpoints ===")
    client.view_api(print_info=True)


if __name__ == "__main__":
    main()
