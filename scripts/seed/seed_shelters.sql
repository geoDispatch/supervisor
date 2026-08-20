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