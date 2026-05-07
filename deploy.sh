sudo systemctl stop contrabass-mole.service
sudo cp -v ~/work/mol/contrabass-moleU /var/lib/contrabass/mole/current
sudo cp -v ~/work/mol/agent.local.yml /var/lib/contrabass/mole/current
sudo systemctl start contrabass-mole.service
