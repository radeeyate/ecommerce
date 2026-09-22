-- Products get an ordered gallery of additional images alongside the
-- single thumbnail they already have. The thumbnail stays where it is:
-- it is the canonical image used in grids, cart lines, emails and
-- og:image, and keeping it a plain column means every one of those call
-- sites is untouched by this change.
--
-- The gallery is a text[] rather than a child table because it is a
-- small, ordered, wholly-owned list that is always read and written as a
-- unit. Array ordinality gives us display order for free, with no
-- position column to keep consistent.
ALTER TABLE public.productcatalog_product
	ADD COLUMN gallery text[] NOT NULL DEFAULT '{}';