-- DEVELOPMENT FIXTURE — shelter records for local testing only. They are
-- NOT official emergency shelters; names and capacities are illustrative.
--
-- This file is mounted into /docker-entrypoint-initdb.d, so it runs only
-- when the Postgres volume is first created. After editing it, recreate the
-- volume (docker compose down -v) or insert the rows by hand.
--
-- Casablanca: 5 shelters near the Casablanca fixture epicentre.
INSERT INTO shelters (name, address, capacity, location) VALUES
(
    'Casablanca Stadium Emergency Shelter',
    'Complexe Mohammed V, Bd Zerktouni, Casablanca',
    5000,
    ST_MakePoint(-7.6230, 33.5950)::geography
),
(
    'Mohamed V Cultural Center Shelter',
    'Av. des F.A.R., Casablanca 20000',
    2000,
    ST_MakePoint(-7.6010, 33.5892)::geography
),
(
    'Ain Diab Community Center',
    'Bd de la Corniche, Ain Diab, Casablanca',
    1500,
    ST_MakePoint(-7.6890, 33.5930)::geography
),
(
    'Lycée Technique de Casablanca',
    'Rue Abderrahmane Sahraoui, Casablanca',
    800,
    ST_MakePoint(-7.5750, 33.5800)::geography
),
(
    'Salle Omnisports de Hay Hassani',
    'Hay Hassani, Casablanca',
    1200,
    ST_MakePoint(-7.6600, 33.5600)::geography
)
ON CONFLICT DO NOTHING;

-- Budapest: 3 shelters near the Budapest fixture epicentre (47.4979,
-- 19.0402). Real public venues at approximate coordinates, marked
-- "(fixture)" because none of them is a designated shelter.
INSERT INTO shelters (name, address, capacity, location) VALUES
(
    'Várkert Bazár (fixture)',
    'Ybl Miklós tér 6, 1013 Budapest',
    1500,
    ST_MakePoint(19.0414, 47.4958)::geography
),
(
    'Millenáris Park (fixture)',
    'Kis Rókus utca 16-20, 1024 Budapest',
    2000,
    ST_MakePoint(19.0265, 47.5105)::geography
),
(
    'Papp László Budapest Sportaréna (fixture)',
    'Stefánia út 2, 1143 Budapest',
    5000,
    ST_MakePoint(19.1047, 47.5027)::geography
)
ON CONFLICT DO NOTHING;
