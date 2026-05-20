from dataclasses import dataclass
from typing import List


@dataclass
class PaginatedResult:
    items: List
    total_count: int
    page: int
    page_size: int

    @property
    def has_next(self) -> bool:
        return (self.page * self.page_size) < self.total_count

    @property
    def has_prev(self) -> bool:
        return self.page > 1
