# ml

Model experimentation and training.

- `notebooks/` – exploration
- `data/` – local datasets (git-ignored)
- `models/` – trained artifacts the backend can load (git-ignored)
- `training/` – training scripts

```bash
python -m venv .venv && source .venv/bin/activate
pip install -r requirements.txt
python training/train.py
```
