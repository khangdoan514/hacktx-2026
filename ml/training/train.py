from pathlib import Path

MODELS_DIR = Path(__file__).resolve().parent.parent / "models"

def main() -> None:
    MODELS_DIR.mkdir(exist_ok=True)
    print(f"TODO: train a model and save it to {MODELS_DIR}")

if __name__ == "__main__":
    main()
