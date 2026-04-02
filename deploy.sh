sudo systemctl stop contrabass-mole.service
sudo cp -v ~/work/mol/contrabass-moleU /var/lib/contrabass/mole/current
sudo cp -v ~/work/mol/config.yaml /var/lib/contrabass/mole/current
sudo systemctl start contrabass-mole.service
