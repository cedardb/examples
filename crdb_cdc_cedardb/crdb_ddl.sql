-- MovR demo

CREATE TABLE public.users
(
	id UUID NOT NULL,
	city VARCHAR NOT NULL,
	name VARCHAR NULL,
	address VARCHAR NULL,
	credit_card VARCHAR NULL,
	CONSTRAINT users_pkey PRIMARY KEY (city ASC, id ASC)
);

CREATE TABLE public.vehicles
(
	id UUID NOT NULL,
	city VARCHAR NOT NULL,
	type VARCHAR NULL,
	owner_id UUID NULL,
	creation_time TIMESTAMP NULL,
	status VARCHAR NULL,
	current_location VARCHAR NULL,
	ext JSONB NULL,
	CONSTRAINT vehicles_pkey PRIMARY KEY (city ASC, id ASC),
	INDEX vehicles_auto_index_fk_city_ref_users (city ASC, owner_id ASC)
);

CREATE TABLE public.rides
(
	id UUID NOT NULL,
	city VARCHAR NOT NULL,
	vehicle_city VARCHAR NULL,
	rider_id UUID NULL,
	vehicle_id UUID NULL,
	start_address VARCHAR NULL,
	end_address VARCHAR NULL,
	start_time TIMESTAMP NULL,
	end_time TIMESTAMP NULL,
	revenue DECIMAL(10,2) NULL,
	CONSTRAINT rides_pkey PRIMARY KEY (city ASC, id ASC),
	INDEX rides_auto_index_fk_city_ref_users (city ASC, rider_id ASC),
	INDEX rides_auto_index_fk_vehicle_city_ref_vehicles (vehicle_city ASC, vehicle_id ASC),
	CONSTRAINT check_vehicle_city_city CHECK (vehicle_city = city) NOT VALID
);

CREATE TABLE public.vehicle_location_histories
(
	city VARCHAR NOT NULL,
	ride_id UUID NOT NULL,
	"timestamp" TIMESTAMP NOT NULL,
	lat FLOAT8 NULL,
	long FLOAT8 NULL,
	CONSTRAINT vehicle_location_histories_pkey PRIMARY KEY (city ASC, ride_id ASC, "timestamp" ASC)
);

CREATE TABLE public.promo_codes
(
	code VARCHAR NOT NULL,
	description VARCHAR NULL,
	creation_time TIMESTAMP NULL,
	expiration_time TIMESTAMP NULL,
	rules JSONB NULL,
	CONSTRAINT promo_codes_pkey PRIMARY KEY (code ASC)
);

CREATE TABLE public.user_promo_codes
(
	city VARCHAR NOT NULL,
	user_id UUID NOT NULL,
	code VARCHAR NOT NULL,
	"timestamp" TIMESTAMP NULL,
	usage_count INT8 NULL,
	CONSTRAINT user_promo_codes_pkey PRIMARY KEY (city ASC, user_id ASC, code ASC)
);

ALTER TABLE public.vehicles ADD CONSTRAINT vehicles_city_owner_id_fkey FOREIGN KEY (city, owner_id) REFERENCES public.users(city, id);

ALTER TABLE public.rides ADD CONSTRAINT rides_city_rider_id_fkey FOREIGN KEY (city, rider_id) REFERENCES public.users(city, id);

ALTER TABLE public.rides ADD CONSTRAINT rides_vehicle_city_vehicle_id_fkey FOREIGN KEY (vehicle_city, vehicle_id) REFERENCES public.vehicles(city, id);

ALTER TABLE public.vehicle_location_histories ADD CONSTRAINT vehicle_location_histories_city_ride_id_fkey FOREIGN KEY (city, ride_id) REFERENCES public.rides(city, id);

ALTER TABLE public.user_promo_codes ADD CONSTRAINT user_promo_codes_city_user_id_fkey FOREIGN KEY (city, user_id) REFERENCES public.users(city, id);

-- Validate foreign key constraints. These can fail if there was unvalidated data during the SHOW CREATE ALL TABLES
ALTER TABLE public.vehicles VALIDATE CONSTRAINT vehicles_city_owner_id_fkey;
ALTER TABLE public.rides VALIDATE CONSTRAINT rides_city_rider_id_fkey;
ALTER TABLE public.rides VALIDATE CONSTRAINT rides_vehicle_city_vehicle_id_fkey;
ALTER TABLE public.vehicle_location_histories VALIDATE CONSTRAINT vehicle_location_histories_city_ride_id_fkey;
ALTER TABLE public.user_promo_codes VALIDATE CONSTRAINT user_promo_codes_city_user_id_fkey;

