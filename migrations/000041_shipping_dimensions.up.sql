-- Shipping needs physical dimensions to get real carrier rates. Weight is
-- in grams and dimensions in millimetres, both stored as integers so no
-- floating-point drift creeps into rate requests. Zero means "unset" —
-- the Shippo adapter falls back to a configurable default parcel for
-- products an operator has not measured yet.
ALTER TABLE public.productcatalog_product
	ADD COLUMN weight_grams int NOT NULL DEFAULT 0,
	ADD COLUMN length_mm int NOT NULL DEFAULT 0,
	ADD COLUMN width_mm int NOT NULL DEFAULT 0,
	ADD COLUMN height_mm int NOT NULL DEFAULT 0;

-- Variants can differ physically from their parent (a large T-shirt
-- weighs more than a small one), so they carry their own measurements.
-- Zero falls back to the parent product's values.
ALTER TABLE public.productcatalog_variant
	ADD COLUMN weight_grams int NOT NULL DEFAULT 0;

-- Order lines snapshot the weight they were quoted at, so re-rating an
-- old order (for a return label, say) uses the historical measurement
-- rather than whatever the catalogue says today.
ALTER TABLE public.checkout_order_item
	ADD COLUMN weight_grams int NOT NULL DEFAULT 0;