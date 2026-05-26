sudo systemctl stop contrabass-mole.service
sudo cp -v ~/work/mol/build/image/contrabass-moleU-compute /var/lib/contrabass/mole/current/contrabass-moleU
sudo cp -v ~/work/mol/cfg/agent.local.yml /var/lib/contrabass/mole/current
sudo systemctl start contrabass-mole.service
