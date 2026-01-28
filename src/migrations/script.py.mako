"""${message}"""

from __future__ import annotations


revision = ${repr(up_revision)}
down_revision = ${repr(down_revision)}
branch_labels = ${repr(branch_labels)}
depends_on = ${repr(depends_on)}


def upgrade() -> None:
    ${upgrades if upgrades else "return None"}


def downgrade() -> None:
    ${downgrades if downgrades else "return None"}

