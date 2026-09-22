ALTER TABLE public.checkout_order_item DROP COLUMN weight_grams;
ALTER TABLE public.productcatalog_variant DROP COLUMN weight_grams;
ALTER TABLE public.productcatalog_product
	DROP COLUMN height_mm,
	DROP COLUMN width_mm,
	DROP COLUMN length_mm,
	DROP COLUMN weight_grams;